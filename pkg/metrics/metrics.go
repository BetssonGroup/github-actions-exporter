package metrics

import (
	"context"
	"fmt"
	"github-actions-exporter/pkg/config"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/v41/github"
	"github.com/gregjones/httpcache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/oauth2"
	"k8s.io/klog/v2"
)

var (
	client                     *github.Client
	err                        error
	workflowRunStatusGauge     *prometheus.GaugeVec
	workflowRunDurationGauge   *prometheus.GaugeVec
	workflowRunStatusStarted   *prometheus.GaugeVec
	workflowRunStatusCompleted *prometheus.GaugeVec
)

// InitMetrics - register metrics in prometheus lib and start func for monitor
func InitMetrics() (chan bool, chan bool) {
	workflowRunStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_status",
			Help: "Workflow run status",
		},
		strings.Split(config.WorkflowFields, ","),
	)
	workflowRunDurationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_duration_ms",
			Help: "Workflow run duration (in milliseconds)",
		},
		strings.Split(config.WorkflowFields, ","),
	)
	workflowRunStatusStarted = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_status_started",
			Help: "Workflow run status started",
		},
		strings.Split(config.WorkflowFields, ","),
	)
	workflowRunStatusCompleted = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_workflow_run_status_completed",
			Help: "Workflow run status completed",
		},
		strings.Split(config.WorkflowFields, ","),
	)

	prometheus.MustRegister(runnersGauge)
	prometheus.MustRegister(runnersOrganizationGauge)
	prometheus.MustRegister(workflowRunStatusGauge)
	prometheus.MustRegister(workflowRunDurationGauge)
	prometheus.MustRegister(workflowRunStatusStarted)
	prometheus.MustRegister(workflowRunStatusCompleted)

	client, err = NewClient()
	if err != nil {
		log.Fatalln("Error: Client creation failed." + err.Error())
	}

	_, resp, err := client.APIMeta(context.Background())
	if _, ok := err.(*github.RateLimitError); ok {
		klog.Errorf("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
		time.Sleep(time.Until(resp.Rate.Reset.Time))
	}

	c := NewCache(client, config.Github.Organizations.Value())
	go c.PreSeedCache()
	repoCache, workflowCache, err := c.Start(time.Duration(config.Github.RepoRefresh)*time.Second, time.Duration(config.Github.WorkflowRefresh)*time.Second)
	if err != nil {
		klog.Exitf("Error: Cache start failed. %s", err.Error())
	}

	go getRunnersFromGithub(c)
	go getRunnersOrganizationFromGithub()
	go getWorkflowRunsFromGithub(c)
	log.Printf("Metrics initialized")
	return repoCache, workflowCache
}

// NewClient creates a Github Client
func NewClient() (*github.Client, error) {
	var (
		httpClient *http.Client
		client     *github.Client
		transport  http.RoundTripper
	)
	if len(config.Github.Token) > 0 {
		klog.Info("Using token auth")
		transport = oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: config.Github.Token})).Transport
		httpClient = &http.Client{Transport: transport}
	} else {
		klog.Info("Using App auth")
		log.Printf("authenticating with Github App")
		tr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, config.Github.AppID, config.Github.AppInstallationID, config.Github.AppPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("authentication failed: %v", err)
		}
		if config.Github.APIURL != "api.github.com" {
			githubAPIURL, err := getEnterpriseApiUrl(config.Github.APIURL)
			if err != nil {
				return nil, fmt.Errorf("enterprise url incorrect: %v", err)
			}
			tr.BaseURL = githubAPIURL
		}
		httpClient = &http.Client{Transport: tr}
	}

	// setup caching in the client
	transport = &httpcache.Transport{
		Transport:           transport,
		Cache:               httpcache.NewMemoryCache(),
		MarkCachedResponses: true,
	}

	httpClient = &http.Client{Transport: transport}

	if config.Github.APIURL != "api.github.com" {
		var err error
		client, err = github.NewEnterpriseClient(config.Github.APIURL, config.Github.APIURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("enterprise client creation failed: %v", err)
		}
	} else {
		client = github.NewClient(httpClient)
	}

	return client, nil
}

func getEnterpriseApiUrl(baseURL string) (string, error) {
	baseEndpoint, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(baseEndpoint.Path, "/") {
		baseEndpoint.Path += "/"
	}
	if !strings.HasSuffix(baseEndpoint.Path, "/api/v3/") &&
		!strings.HasPrefix(baseEndpoint.Host, "api.") &&
		!strings.Contains(baseEndpoint.Host, ".api.") {
		baseEndpoint.Path += "api/v3/"
	}

	// Trim trailing slash, otherwise there's double slash added to token endpoint
	return fmt.Sprintf("%s://%s%s", baseEndpoint.Scheme, baseEndpoint.Host, strings.TrimSuffix(baseEndpoint.Path, "/")), nil
}
