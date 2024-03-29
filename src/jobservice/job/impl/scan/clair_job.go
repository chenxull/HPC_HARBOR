// Copyright Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/goharbor/harbor/src/common"
	"github.com/goharbor/harbor/src/common/dao"
	"github.com/goharbor/harbor/src/common/job"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils/clair"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/jobservice/env"
	"github.com/goharbor/harbor/src/jobservice/job/impl/utils"
)

// ClairJob is the struct to scan Harbor's Image with Clair
type ClairJob struct {
	registryURL   string
	secret        string
	tokenEndpoint string
	clairEndpoint string
}

// MaxFails implements the interface in job/Interface
func (cj *ClairJob) MaxFails() uint {
	return 1
}

// ShouldRetry implements the interface in job/Interface
func (cj *ClairJob) ShouldRetry() bool {
	return false
}

// Validate implements the interface in job/Interface
func (cj *ClairJob) Validate(params map[string]interface{}) error {
	return nil
}

// Run implements the interface in job/Interface
func (cj *ClairJob) Run(ctx env.JobContext, params map[string]interface{}) error {
	logger := ctx.GetLogger()
	// 获取 clair 结构体中定义数据的配置信息
	if err := cj.init(ctx); err != nil {
		logger.Errorf("Failed to initialize the job, error: %v", err)
		return err
	}

	// 解析job 参数
	jobParms, err := transformParam(params)
	if err != nil {
		logger.Errorf("Failed to prepare parms for scan job, error: %v", err)
		return err
	}
	//创建访问存储库的客户端
	repoClient, err := utils.NewRepositoryClientForJobservice(jobParms.Repository, cj.registryURL, cj.secret, cj.tokenEndpoint)
	if err != nil {
		logger.Errorf("Failed create repository client for repo: %s, error: %v", jobParms.Repository, err)
		return err
	}
	// 获取镜像的manifest，这里含有镜像的概要信息
	_, _, payload, err := repoClient.PullManifest(jobParms.Tag, []string{schema2.MediaTypeManifest})
	if err != nil {
		logger.Errorf("Error pulling manifest for image %s:%s :%v", jobParms.Repository, jobParms.Tag, err)
		return err
	}
	// 想要访问的存储库，cj 的密码，以及 cj 的 token 服务器地址作为参数来获取访问存储器的 token 信息
	token, err := utils.GetTokenForRepo(jobParms.Repository, cj.secret, cj.tokenEndpoint)
	if err != nil {
		logger.Errorf("Failed to get token, error: %v", err)
		return err
	}
	layers, err := prepareLayers(payload, cj.registryURL, jobParms.Repository, token)
	if err != nil {
		logger.Errorf("Failed to prepare layers, error: %v", err)
		return err
	}
	loggerImpl, ok := logger.(*log.Logger)
	if !ok {
		loggerImpl = log.DefaultLogger()
	}
	// 创建访问 clair 的 client
	clairClient := clair.NewClient(cj.clairEndpoint, loggerImpl)

	for _, l := range layers {
		logger.Infof("Scanning Layer: %s, path: %s", l.Name, l.Path)
		// 将所有层都提交给 clair
		if err := clairClient.ScanLayer(l); err != nil {
			logger.Errorf("Failed to scan layer: %s, error: %v", l.Name, err)
			return err
		}
	}

	layerName := layers[len(layers)-1].Name
	res, err := clairClient.GetResult(layerName)
	if err != nil {
		logger.Errorf("Failed to get result from Clair, error: %v", err)
		return err
	}
	// 漏洞概要信息
	compOverview, sev := clair.TransformVuln(res)
	// 将漏洞信息存储到数据库中
	err = dao.UpdateImgScanOverview(jobParms.Digest, layerName, sev, compOverview)
	return err
}

func (cj *ClairJob) init(ctx env.JobContext) error {
	errTpl := "Failed to get required property: %s"
	if v, ok := ctx.Get(common.RegistryURL); ok && len(v.(string)) > 0 {
		cj.registryURL = v.(string)
	} else {
		return fmt.Errorf(errTpl, common.RegistryURL)
	}

	if v := os.Getenv("JOBSERVICE_SECRET"); len(v) > 0 {
		cj.secret = v
	} else {
		return fmt.Errorf(errTpl, "JOBSERVICE_SECRET")
	}
	if v, ok := ctx.Get(common.TokenServiceURL); ok && len(v.(string)) > 0 {
		cj.tokenEndpoint = v.(string)
	} else {
		return fmt.Errorf(errTpl, common.TokenServiceURL)
	}
	if v, ok := ctx.Get(common.ClairURL); ok && len(v.(string)) > 0 {
		cj.clairEndpoint = v.(string)
	} else {
		return fmt.Errorf(errTpl, common.ClairURL)
	}
	return nil
}

func transformParam(params map[string]interface{}) (*job.ScanJobParms, error) {
	res := job.ScanJobParms{}
	parmsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(parmsBytes, &res)
	return &res, err
}

func prepareLayers(payload []byte, registryURL, repo, tk string) ([]models.ClairLayer, error) {
	layers := []models.ClairLayer{}
	manifest, _, err := distribution.UnmarshalManifest(schema2.MediaTypeManifest, payload)
	if err != nil {
		return layers, err
	}
	tokenHeader := map[string]string{"Connection": "close", "Authorization": fmt.Sprintf("Bearer %s", tk)}
	// form the chain by using the digests of all parent layers in the image, such that if another image is built on top of this image the layer name can be re-used.
	shaChain := ""
	for _, d := range manifest.References() {
		if d.MediaType == schema2.MediaTypeConfig {
			continue
		}
		shaChain += string(d.Digest) + "-"
		l := models.ClairLayer{
			Name:    fmt.Sprintf("%x", sha256.Sum256([]byte(shaChain))),
			Headers: tokenHeader,
			Format:  "Docker",
			Path:    utils.BuildBlobURL(registryURL, repo, string(d.Digest)),
		}
		if len(layers) > 0 {
			l.ParentName = layers[len(layers)-1].Name
		}
		layers = append(layers, l)
	}
	return layers, nil
}
