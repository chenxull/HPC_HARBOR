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

package pool

import (
	"reflect"

	"github.com/goharbor/harbor/src/jobservice/job"
)

// Wrap returns a new job.Interface based on the wrapped job handler reference.
func Wrap(j interface{}) job.Interface {
	// theType 是此结构体的名称，获取 j 的动态类型
	theType := reflect.TypeOf(j)

	// 如说是指针类型，将其转化为元素类型
	if theType.Kind() == reflect.Ptr {
		// 将指针类型所指向的实际数据赋值给 Type
		theType = theType.Elem()
	}

	// Crate new
	// 创建一个类型为 theType 的新的指针类型的数据
	v := reflect.New(theType).Elem()
	// 其是符合 job Interface 接口的
	return v.Addr().Interface().(job.Interface)
}
