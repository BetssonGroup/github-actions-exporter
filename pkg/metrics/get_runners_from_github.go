package metrics

import (
	"context"
	"github-actions-exporter/pkg/config"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v41/github"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	runnersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_runner_status",
			Help: "runner status",
		},
		[]string{"repo", "os", "name", "id", "labels", "busy"},
	)
)

// getRunnersFromGithub - return information about runners and their status for a specific repo
func getRunnersFromGithub(cache *InMemCache) {
	// return a commaseparated string of label values
	runnerLabels := func(labels []*github.RunnerLabels) string {
		labelString := ""
		for _, label := range labels {
			labelString += label.GetName() + ","
		}
		return strings.TrimSuffix(labelString, ",")
	}

	for {
		for repo := range cache.WorkflowCache.Items() {
			r := strings.Split(repo, "/")
			resp, _, err := client.Actions.ListRunners(context.Background(), r[0], r[1], nil)
			if err != nil {
				log.Printf("ListRunners error for %s: %s", repo, err.Error())
			} else {
				for _, runner := range resp.Runners {

					if runner.GetStatus() == "online" {
						runnersGauge.WithLabelValues(repo, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), runnerLabels(runner.Labels), strconv.FormatBool(runner.GetBusy())).Set(1)
					} else {
						runnersGauge.WithLabelValues(repo, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), runnerLabels(runner.Labels), strconv.FormatBool(runner.GetBusy())).Set(0)
					}
				}
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}
