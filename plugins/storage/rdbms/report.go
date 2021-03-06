// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package rdbms

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/facebookincubator/contest/pkg/job"
	"github.com/facebookincubator/contest/pkg/types"
)

// StoreJobReport persists the job report on the internal storage.
func (r *RDBMS) StoreJobReport(jobReport *job.JobReport) error {
	if err := r.init(); err != nil {
		return fmt.Errorf("could not initialize database: %v", err)
	}

	for runID, runReports := range jobReport.RunReports {
		for _, report := range runReports {
			insertStatement := "insert into run_reports (job_id, run_number, success, report_time, data) values (?, ?, ?, ?, ?)"
			reportJSON, err := report.ToJSON()
			if err != nil {
				return fmt.Errorf("could not serialize run report for job %v: %v", jobReport.JobID, err)
			}
			// note: run ID is a zero-based index, while the run number starts
			// at 1 (hence the +1). We store the run number, not the run ID. A
			// zero value means that something is wrong.
			if _, err := r.db.Exec(insertStatement, jobReport.JobID, runID+1, report.Success, report.ReportTime, reportJSON); err != nil {
				return fmt.Errorf("could not store run report for job %v: %v", jobReport.JobID, err)
			}
		}
	}
	for _, report := range jobReport.FinalReports {
		insertStatement := "insert into final_reports (job_id, success, report_time, data) values (?, ?, ?, ?)"
		reportJSON, err := report.ToJSON()
		if err != nil {
			return fmt.Errorf("could not serialize final report for job %v: %v", jobReport.JobID, err)
		}
		// note: run ID is a zero-based index, while the run number starts
		// at 1 (hence the +1). We store the run number, not the run ID.
		if _, err := r.db.Exec(insertStatement, jobReport.JobID, report.Success, report.ReportTime, reportJSON); err != nil {
			return fmt.Errorf("could not store final report for job %v: %v", jobReport.JobID, err)
		}
	}
	return nil
}

// GetJobReport retrieves a JobReport from the database
func (r *RDBMS) GetJobReport(jobID types.JobID) (*job.JobReport, error) {
	if err := r.init(); err != nil {
		return nil, fmt.Errorf("could not initialize database: %v", err)
	}

	var (
		runReports        [][]*job.Report
		currentRunReports []*job.Report
		finalReports      []*job.Report
	)

	// get run reports. Don't change the order by asc, because
	// the code below assumes sorted results by ascending run number.
	selectStatement := "select success, report_time, run_number, data from run_reports where job_id = ? order by run_number asc"
	log.Debugf("Executing query: %s", selectStatement)
	rows, err := r.db.Query(selectStatement, jobID)
	if err != nil {
		return nil, fmt.Errorf("could not get run report for job %v: %v", jobID, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warningf("failed to close rows from query statement: %v", err)
		}
	}()
	var lastRunNum, currentRunNum uint
	for rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("could not fetch run report for job %d: %v", jobID, err)
		}
		var (
			report job.Report
			data   string
		)
		err = rows.Scan(
			&report.Success,
			&report.ReportTime,
			&currentRunNum,
			&data,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row while fetching run report for job %d: %v", jobID, err)
		}
		if err := json.Unmarshal([]byte(data), &report.Data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal run report JSON data: %v", err)
		}
		// rows are sorted by ascending run_number, so if we find a
		// non-monotonic run_number or a gap, we return an error.
		// This works as long as we can assume ascending sorting, so don't
		// change it, or at least change both.
		if currentRunNum == 0 {
			return nil, errors.New("invalid run_number in database, cannot be zero")
		}
		if currentRunNum < lastRunNum || currentRunNum > lastRunNum+1 {
			return nil, fmt.Errorf("invalid run_number retrieved from database: either it is not ordered, or there is a gap in run numbers in the database for job %d. Current run number: %d, last run number: %d",
				jobID, currentRunNum, lastRunNum,
			)
		}
		if currentRunNum != lastRunNum {
			// this is the next run number
			if lastRunNum > 0 {
				runReports = append(runReports, currentRunReports)
				currentRunReports = make([]*job.Report, 0)
			}
			lastRunNum = currentRunNum
		}
		currentRunReports = append(currentRunReports, &report)
	}
	if len(currentRunReports) > 0 {
		runReports = append(runReports, currentRunReports)
	}

	// get final reports
	selectStatement = "select success, report_time, data from final_reports where job_id = ?"
	log.Debugf("Executing query: %s", selectStatement)
	rows, err = r.db.Query(selectStatement, jobID)
	if err != nil {
		return nil, fmt.Errorf("could not get final report for job %v: %v", jobID, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warningf("failed to close rows from query statement: %v", err)
		}
	}()
	for rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("could not fetch final report for job %d: %v", jobID, err)
		}
		var (
			report job.Report
			data   string
		)
		err = rows.Scan(
			&report.Success,
			&report.ReportTime,
			&data,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row while fetching final report for job %d: %v", jobID, err)
		}
		if err := json.Unmarshal([]byte(data), &report.Data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal final report JSON data: %v", err)
		}
		finalReports = append(finalReports, &report)
	}
	return &job.JobReport{
		JobID:        jobID,
		RunReports:   runReports,
		FinalReports: finalReports,
	}, nil
}
