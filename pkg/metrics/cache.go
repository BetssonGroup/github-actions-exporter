package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/google/go-github/v41/github"
	gocache "github.com/patrickmn/go-cache"
	"k8s.io/klog/v2"
)

type Cache interface {
	CacheRepos(org string) error
}

type InMemCache struct {
	RepoCache     *gocache.Cache
	WorkflowCache *gocache.Cache
	GhClient      *github.Client
	Organizations []string
}

type repoCacheItem struct {
	repo         *github.Repository
	hasWorkflows bool
	recheckTime  time.Time
}

func NewCache(client *github.Client, organizations []string) *InMemCache {
	return &InMemCache{
		RepoCache:     gocache.New(gocache.NoExpiration, gocache.NoExpiration),
		WorkflowCache: gocache.New(gocache.NoExpiration, gocache.NoExpiration),
		GhClient:      client,
		Organizations: organizations,
	}
}

func (c *InMemCache) PreSeedCache() error {
	klog.Info("pre-seeding cache")
	c.CacheRepos()
	c.CacheWorkflows()
	klog.Info("pre-seeding cache complete")
	return nil
}

func (c *InMemCache) Start(repoCacheInterval time.Duration, workflowCacheInterval time.Duration) (chan bool, chan bool, error) {
	repoTicker := time.NewTicker(time.Duration(repoCacheInterval))
	workflowTicker := time.NewTicker(time.Duration(workflowCacheInterval))
	stopRepo := make(chan bool)
	stopWorkflow := make(chan bool)
	go func() {
		klog.Infoln("starting repo cache job")
		for {
			select {
			case <-stopRepo:
				return
			case <-repoTicker.C:
				c.CacheRepos()
			}
		}
	}()
	go func() {
		klog.Infoln("starting workflow cache job")
		for {
			select {
			case <-stopWorkflow:
				return
			case <-workflowTicker.C:
				c.CacheWorkflows()
			}
		}
	}()
	return stopRepo, stopWorkflow, nil

}

func (c *InMemCache) getRepos() []*github.Repository {
	var (
		repos []*github.Repository
		resp  *github.Response
	)

	options := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	for _, org := range c.Organizations {
		for {
			r, resp, err := c.GhClient.Repositories.ListByOrg(context.Background(), org, options)
			if err != nil {
				if _, ok := err.(*github.RateLimitError); ok {
					klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
					time.Sleep(time.Until(resp.Rate.Reset.Time))
					continue
				}
				klog.Errorf("error getting repos for org %s: %v", org, err)
				break
			}
			repos = append(repos, r...)
			if resp.NextPage == 0 {
				break
			}
			options.ListOptions.Page = resp.NextPage
		}
	}
	klog.V(4).Infof("found %d repos", len(repos))
	_, resp, err := client.APIMeta(context.Background())
	if err != nil {
		klog.Errorf("error getting rate limit: %v", err)
	} else {
		klog.Errorf("API rate limit: %d/%d", resp.Rate.Remaining, resp.Rate.Limit)
	}
	return repos
}

func (c *InMemCache) CacheRepos() {
	repos := c.getRepos()
	for _, repo := range repos {
		c.RepoCache.Set(repo.GetFullName(), &repoCacheItem{
			repo:         repo,
			hasWorkflows: c.hasWorkflows(repo.GetFullName()),
			recheckTime:  time.Now(), // recheck imidiately
		}, gocache.NoExpiration)
	}
	for repo := range c.RepoCache.Items() {
		if !c.containsRepo(repos, repo) {
			c.RepoCache.Delete(repo)
		}
	}
}

func (c *InMemCache) containsRepo(repos []*github.Repository, repo string) bool {
	for _, r := range repos {
		if r.GetFullName() == repo {
			return true
		}
	}
	klog.V(4).Infof("repo %s not found in list of repos", repo)
	return false
}

func (c *InMemCache) hasWorkflows(repo string) bool {
	orgRepo := strings.Split(repo, "/")
	w, resp, err := client.Actions.ListWorkflows(context.Background(), orgRepo[0], orgRepo[1], nil)
	if err != nil {
		if _, ok := err.(*github.RateLimitError); ok {
			klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
			time.Sleep(time.Until(resp.Rate.Reset.Time))
			return false
		}
		klog.Errorf("error getting workflows for repo %s: %s", repo, err)
		return false
	}
	return w.GetTotalCount() > 0
}

func (c *InMemCache) getWorkflowsForRepo(repo *repoCacheItem) ([]*github.Workflow, error) {
	var workflows []*github.Workflow
	options := &github.ListOptions{
		PerPage: 100,
	}
	orgRepo := strings.Split(*repo.repo.FullName, "/")
	for {
		w, resp, err := client.Actions.ListWorkflows(context.Background(), orgRepo[0], orgRepo[1], options)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", resp.Rate.Reset.Format(time.RFC3339))
				time.Sleep(time.Until(resp.Rate.Reset.Time))
				continue
			}
			klog.Errorf("error getting workflows for repo %s: %s", repo, err)
		}
		workflows = append(workflows, w.Workflows...)
		if resp.NextPage == 0 {
			break
		}
		options.Page = resp.NextPage
	}
	return workflows, nil
}

func (c *InMemCache) CacheWorkflows() {
	klog.Info("caching workflows...")
	for repo, item := range c.RepoCache.Items() {
		if item.Object.(*repoCacheItem).hasWorkflows && item.Object.(*repoCacheItem).recheckTime.Before(time.Now()) {
			klog.V(8).Infof("checking for workflows for repo %s", repo)
			wf, err := c.getWorkflowsForRepo(item.Object.(*repoCacheItem))
			if err != nil {
				if _, ok := err.(*github.RateLimitError); ok {
					klog.Infof("hit rate limit, sleeping until rate limit reset (%s)", err.(*github.RateLimitError).Rate.Reset.Format(time.RFC3339))
					time.Sleep(time.Until(err.(*github.RateLimitError).Rate.Reset.Time))
					continue
				}
				klog.Errorf("error getting workflows for repo %s: %s", repo, err)
				continue
			}
			s := make(map[int64]github.Workflow)
			for _, w := range wf {
				s[*w.ID] = *w
			}
			c.WorkflowCache.Set(repo, s, gocache.NoExpiration)
			item.Object.(*repoCacheItem).recheckTime = time.Now().Add(time.Second * 60)
			c.RepoCache.Set(repo, item.Object.(*repoCacheItem), gocache.NoExpiration)
			klog.V(8).Infof("cached %d workflows for repo %s", len(s), repo)
		} else {
			klog.V(10).Infof("skipping repo %s", repo)
			// set recheck
			item.Object.(*repoCacheItem).recheckTime = time.Now().Add(time.Duration(300) * time.Second)
			c.RepoCache.Set(repo, item.Object, gocache.NoExpiration)
		}
	}
	c.cleanupWorkflows()
	klog.V(4).Infof("cached %d workflows for repos", len(c.WorkflowCache.Items()))
	_, resp, err := client.APIMeta(context.Background())
	if err != nil {
		klog.Errorf("error getting rate limit: %v", err)
	} else {
		klog.Errorf("API rate limit: %d/%d", resp.Rate.Remaining, resp.Rate.Limit)
	}
}

func (c *InMemCache) cleanupWorkflows() {
	for repo := range c.WorkflowCache.Items() {
		if _, ok := c.RepoCache.Get(repo); !ok {
			klog.V(4).Infof("deleting workflow cache for repo %s", repo)
			c.WorkflowCache.Delete(repo)
		}
	}
}
