package metrics

import (
	"context"
	"github-actions-exporter/pkg/config"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v41/github"
	"k8s.io/klog/v2"
)

// getFieldValue return value from run element which corresponds to field
func getFieldValue(repo string, run github.WorkflowRun, job github.WorkflowJob, field string) string {
	switch field {
	case "repo":
		return repo
	case "id":
		return strconv.FormatInt(*run.ID, 10)
	case "head_branch":
		return run.GetHeadBranch()
	case "run_number":
		return strconv.Itoa(*run.RunNumber)
	case "workflow":
		return run.GetName()
	case "event":
		return run.GetEvent()
	case "status":
		return run.GetStatus()
	case "runner_name":
		return job.GetRunnerName()
	case "job_name":
		return job.GetName()
	case "job_status":
		return *job.Status
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
func getWorkflowRunsFromGithub(cache *InMemCache) {
	for {
		for repo := range cache.WorkflowCache.Items() {
			r := strings.Split(repo, "/")
			workflowList, resp, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), r[0], r[1], &github.ListWorkflowRunsOptions{
				ListOptions: github.ListOptions{
					Page:    1,
					PerPage: 100,
				},
			})
			if err != nil {
				if _, ok := err.(*github.RateLimitError); ok {
					klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
					time.Sleep(time.Until(resp.Rate.Reset.Time))
				}
				klog.Errorf("ListRepositoryWorkflowRuns error for %s: %s", repo, err.Error())
			} else {
				for _, run := range workflowList.WorkflowRuns {
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
					// only get runs from the last 24h
					if run.CreatedAt.Time.After(time.Now().Add(-time.Duration(24) * time.Hour)) {
						jobs, resp, err := client.Actions.ListWorkflowJobs(context.Background(), r[0], r[1], *run.ID, &github.ListWorkflowJobsOptions{})
						if err != nil {
							if _, ok := err.(*github.RateLimitError); ok {
								klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
								time.Sleep(time.Until(resp.Rate.Reset.Time))
							}
							log.Printf("ListWorkflowJobs error for %s: %s", repo, err.Error())
							continue
						}

						for _, job := range jobs.Jobs {
							if jobs.GetTotalCount() > 0 {
								fields := getRelevantFields(repo, run, job)
								workflowRunStatusGauge.WithLabelValues(fields...).Set(s)
								workflowRunStatusStarted.WithLabelValues(fields...).Set(float64(job.StartedAt.Time.Unix()))
								if job.CompletedAt != nil {
									workflowRunStatusCompleted.WithLabelValues(fields...).Set(float64(job.CompletedAt.Time.Unix()))
								}
								usage, resp, err := client.Actions.GetWorkflowRunUsageByID(context.Background(), r[0], r[1], *run.ID)
								if err != nil {
									if _, ok := err.(*github.RateLimitError); ok {
										klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
										time.Sleep(time.Until(resp.Rate.Reset.Time))
									}
									klog.Errorf("GetWorkflowRunUsageByID error for %s: %s", repo, err.Error())
									continue
								} else {
									workflowRunDurationGauge.WithLabelValues(fields...).Set(float64(usage.GetRunDurationMS()))

								}
							}
						}
					}
				}
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}
