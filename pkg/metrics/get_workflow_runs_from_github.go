package metrics

import (
	"context"
	"github-actions-exporter/pkg/config"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v41/github"
)

// getFieldValue return value from run element which corresponds to field
func getFieldValue(repo string, run github.WorkflowRun, job github.WorkflowJob, field string) string {
	switch field {
	case "repo":
		return repo
	case "id":
		return strconv.FormatInt(*run.ID, 10)
	case "node_id":
		return run.GetNodeID()
	case "head_branch":
		return *run.HeadBranch
	case "head_sha":
		return *run.HeadSHA
	case "run_number":
		return strconv.Itoa(*run.RunNumber)
	case "workflow_id":
		return strconv.FormatInt(*run.WorkflowID, 10)
	case "workflow":
		return *workflows[repo][*run.WorkflowID].Name
	case "event":
		return *run.Event
	case "status":
		return *run.Status
	case "runner":
		return job.GetRunnerName()

	}
	return ""
}

//
func getRelevantFields(repo string, run *github.WorkflowRun, job *github.WorkflowJob) []string {
	relevantFields := strings.Split(config.WorkflowFields, ",")
	result := make([]string, len(relevantFields))
	for i, field := range relevantFields {
		result[i] = getFieldValue(repo, *run, *job, field)
	}
	return result
}

// getWorkflowRunsFromGithub - return informations and status about a worflow
func getWorkflowRunsFromGithub() {
	for {
		for repo := range workflows {
			r := strings.Split(repo, "/")
			resp, _, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), r[0], r[1], nil)
			if err != nil {
				log.Printf("ListRepositoryWorkflowRuns error for %s: %s", repo, err.Error())
			} else {
				for _, run := range resp.WorkflowRuns {
					var s float64 = 0
					if run.GetConclusion() == "success" {
						s = 1
					} else if run.GetConclusion() == "skipped" {
						s = 2
					} else if run.GetConclusion() == "in_progress" {
						s = 3
					} else if run.GetConclusion() == "queued" {
						s = 4
					}

					jobs, _, err := client.Actions.ListWorkflowJobs(context.Background(), r[0], r[1], *run.ID, nil)
					if err != nil {
						log.Printf("ListWorkflowJobs error for %s: %s", repo, err.Error())
						jobs = &github.Jobs{}
					}

					for _, job := range jobs.Jobs {

						fields := getRelevantFields(repo, run, job)

						workflowRunStatusGauge.WithLabelValues(fields...).Set(s)

						resp, _, err := client.Actions.GetWorkflowRunUsageByID(context.Background(), r[0], r[1], *run.ID)
						if err != nil { // Fallback for Github Enterprise
							created := run.CreatedAt.Time.Unix()
							updated := run.UpdatedAt.Time.Unix()
							elapsed := updated - created
							workflowRunDurationGauge.WithLabelValues(fields...).Set(float64(elapsed * 1000))
						} else {
							workflowRunDurationGauge.WithLabelValues(fields...).Set(float64(resp.GetRunDurationMS()))
						}

					}
				}
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}
