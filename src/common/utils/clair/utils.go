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

package clair

import (
	"fmt"
	"github.com/goharbor/harbor/src/common/dao"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils/log"
	"strings"
)

// var client = NewClient()

// ParseClairSev parse the severity of clair to Harbor's Severity type if the string is not recognized the value will be set to unknown.
func ParseClairSev(clairSev string) models.Severity {
	sev := strings.ToLower(clairSev)
	switch sev {
	case models.SeverityNone:
		return models.SevNone
	case models.SeverityLow:
		return models.SevLow
	case models.SeverityMedium:
		return models.SevMedium
	case models.SeverityHigh, models.SeverityCritical:
		return models.SevHigh
	default:
		return models.SevUnknown
	}
}

// UpdateScanOverview qeuries the vulnerability based on the layerName and update the record in img_scan_overview table based on digest.
func UpdateScanOverview(digest, layerName string, clairEndpoint string, l ...*log.Logger) error {
	var logger *log.Logger
	if len(l) > 1 {
		return fmt.Errorf("More than one logger specified")
	} else if len(l) == 1 {
		logger = l[0]
	} else {
		logger = log.DefaultLogger()
	}
	client := NewClient(clairEndpoint, logger)
	// 获取此层的漏洞信息
	res, err := client.GetResult(layerName)
	if err != nil {
		logger.Errorf("Failed to get result from Clair, error: %v", err)
		return err
	}
	compOverview, sev := transformVuln(res)
	// 更新img_scan_overview中的components和serverity记录
	return dao.UpdateImgScanOverview(digest, layerName, sev, compOverview)
}

//transformVuln()函数用来获取总的漏洞数量以及各个漏洞级别漏洞的数量
func transformVuln(clairVuln *models.ClairLayerEnvelope) (*models.ComponentsOverview, models.Severity) {
	vulnMap := make(map[models.Severity]int)
	// 获取此层中所有的漏洞的信息,features 的结构体如下
	/*
		// ClairFeature ...
		type ClairFeature struct {
		Name            string               `json:"Name,omitempty"`
		NamespaceName   string               `json:"NamespaceName,omitempty"`
		VersionFormat   string               `json:"VersionFormat,omitempty"`
		Version         string               `json:"Version,omitempty"`
		Vulnerabilities []ClairVulnerability `json:"Vulnerabilities,omitempty"`
		AddedBy         string               `json:"AddedBy,omitempty"`
	}
	*/
	features := clairVuln.Layer.Features
	totalComponents := len(features)
	var temp models.Severity
	// 为次层中的每个漏洞划分漏洞风险级别，从 1 到 5
	for _, f := range features {
		sev := models.SevNone
		for _, v := range f.Vulnerabilities {
			temp = ParseClairSev(v.Severity)
			if temp > sev {
				sev = temp
			}
		}
		// vulnMap 用来统计每种级别的漏洞有多少个，需要在前端上展示出来。
		vulnMap[sev]++
	}
	overallSev := models.SevNone
	compSummary := []*models.ComponentsOverviewEntry{}
	for k, v := range vulnMap {
		if k > overallSev {
			overallSev = k
		}
		entry := &models.ComponentsOverviewEntry{
			Sev:   int(k),
			Count: v,
		}
		compSummary = append(compSummary, entry)
	}
	return &models.ComponentsOverview{
		Total:   totalComponents,
		Summary: compSummary, // 每种级别的漏洞各个多少个
	}, overallSev
}

// TransformVuln is for running scanning job in both job service V1 and V2.
func TransformVuln(clairVuln *models.ClairLayerEnvelope) (*models.ComponentsOverview, models.Severity) {
	return transformVuln(clairVuln)
}
