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

package models

import (
	"time"
)

const (
	// AdminJobTable is table name for admin job
	// 表名admin_job
	AdminJobTable = "admin_job"
)

// AdminJob ...
type AdminJob struct {
	ID           int64     `orm:"pk;auto;column(id)" json:"id"`
	Name         string    `orm:"column(job_name)"  json:"job_name"`
	Kind         string    `orm:"column(job_kind)"  json:"job_kind"`
	Cron         string    `orm:"column(cron_str)"  json:"cron_str"`
	Status       string    `orm:"column(status)"  json:"job_status"`
	UUID         string    `orm:"column(job_uuid)" json:"-"`
	Deleted      bool      `orm:"column(deleted)" json:"deleted"`
	CreationTime time.Time `orm:"column(creation_time);auto_now_add" json:"creation_time"`
	UpdateTime   time.Time `orm:"column(update_time);auto_now" json:"update_time"`
}

// TableName is required by by beego orm to map AdminJob to table AdminJob
func (a *AdminJob) TableName() string {
	return AdminJobTable
}

// AdminJobQuery : query parameters for adminjob
type AdminJobQuery struct {
	ID      int64
	Name    string
	Kind    string
	Status  string
	UUID    string
	Deleted bool
	Pagination
}
