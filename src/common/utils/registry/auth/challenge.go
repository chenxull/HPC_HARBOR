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

package auth

import (
	"net/http"

	"github.com/docker/distribution/registry/client/auth/challenge"
)

// ParseChallengeFromResponse ...
// Challenge-Response协议
//基于挑战/应答（Challenge/Response）方式的身份认证系统就是每次认证时认证服务器端都给客户端发送一个不同的"挑战"字串，
// 客户端程序收到这个"挑战"字串后，做出相应的"应答",以此机制而研制的系统.
func ParseChallengeFromResponse(resp *http.Response) []challenge.Challenge {
	challenges := challenge.ResponseChallenges(resp)

	return challenges
}
