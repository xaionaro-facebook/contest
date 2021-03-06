// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package job

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/facebookincubator/contest/pkg/types"
)

// Report wraps the information of a run report or a final report.
type Report struct {
	Success    bool
	ReportTime time.Time
	Data       interface{}
}

// JobReport represents the whole job report generated by ConTest.
type JobReport struct {
	JobID types.JobID
	// JobReport represents the report generated by the plugin selected in the job descriptor
	RunReports   [][]*Report
	FinalReports []*Report
}

// ToJSON marshals the report into JSON, disabling HTML escaping
func (r *Report) ToJSON() ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(r.Data)
	return buffer.Bytes(), err
}

// ReportEmitter is an interface implemented by objects that implement report emission logic
type ReportEmitter interface {
	Emit(jobReport *JobReport) error
}

// ReportFetcher is an interface implemented by objects that implement report fetching logic
type ReportFetcher interface {
	Fetch(jobID types.JobID) (*JobReport, error)
}

// ReportEmitterFetcher is an interface implemented by objects the implement report emission
// end fetching logic
type ReportEmitterFetcher interface {
	ReportEmitter
	ReportFetcher
}
