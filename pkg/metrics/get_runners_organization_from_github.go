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
	"k8s.io/klog/v2"
)

var (
	runnersOrganizationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_runner_organization_status",
			Help: "runner status",
		},
		[]string{"organization", "os", "name", "id", "labels", "busy"},
	)
)

// getRunnersOrganizationFromGithub - return information about runners and their status for an organization
func getRunnersOrganizationFromGithub() {
	runnerLabels := func(labels []*github.RunnerLabels) string {
		labelString := ""
		for _, label := range labels {
			labelString += label.GetName() + ","
		}
		return strings.TrimSuffix(labelString, ",")
	}
	for {
		for _, org := range config.Github.Organizations.Value() {
			opt := &github.ListOptions{PerPage: 100}
			for {
				runners, resp, err := client.Actions.ListOrganizationRunners(context.Background(), org, opt)
				if err != nil {
					if _, ok := err.(*github.RateLimitError); ok {
						klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
						time.Sleep(time.Until(resp.Rate.Reset.Time))
						continue
					}
					klog.Errorf("error getting repos for org %s: %v", org, err)
					log.Printf("ListOrganizationRunners error for %s: %s", org, err.Error())
				} else {
					for _, runner := range runners.Runners {
						if runner.GetStatus() == "online" {
							runnersOrganizationGauge.WithLabelValues(org, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), runnerLabels(runner.Labels), strconv.FormatBool(runner.GetBusy())).Set(1)
						} else {
							runnersOrganizationGauge.WithLabelValues(org, *runner.OS, *runner.Name, strconv.FormatInt(runner.GetID(), 10), runnerLabels(runner.Labels), strconv.FormatBool(runner.GetBusy())).Set(0)
						}
					}
					if resp.NextPage == 0 {
						break
					}
					opt.Page = resp.NextPage
				}
			}
		}

		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
		runnersOrganizationGauge.Reset()
	}
}
