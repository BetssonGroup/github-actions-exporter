package metrics

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/google/go-github/v41/github"

	"github-actions-exporter/pkg/config"

	"github.com/patrickmn/go-cache"
	gocache "github.com/patrickmn/go-cache"
)

var (
	workflows map[string]map[int64]github.Workflow
	Cache     *gocache.Cache
)

type repoCache struct {
	repo         *github.Repository
	hasWorkflows bool
	recheckTime  time.Time
}

// make two caches, poplate them and then star 2 cache goroutines https://github.com/patrickmn/go-cache
func listOrgRepos(org string) ([]github.Repository, error) {
	log.Printf("Listing repos for %s", org)
	var ret []github.Repository
	var err error
	repos, resp, err := client.Repositories.ListByOrg(context.Background(), org, &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		log.Printf("ListByOrg error for %s: %s", org, err.Error())
		return ret, err
	}
	for _, repo := range repos {
		ret = append(ret, *repo)
	}
	for resp.NextPage != 0 {
		repos, resp, err = client.Repositories.ListByOrg(context.Background(), org, &github.RepositoryListByOrgOptions{
			ListOptions: github.ListOptions{
				PerPage: 100,
				Page:    resp.NextPage,
			},
		})
		if err != nil {
			log.Printf("ListByOrg error for %s: %s", org, err.Error())
			return ret, err
		}
		for _, repo := range repos {
			ret = append(ret, *repo)
		}
	}
	log.Printf("ListByOrg for %s: %d", org, len(ret))
	return ret, nil
}

func NewRepoCache() *gocache.Cache {
	return gocache.New(gocache.NoExpiration, gocache.NoExpiration)
}

func cacheRepos(recheck time.Duration) {
	for {
		if config.Github.Repositories.Value() != nil {
			log.Printf("Refresh repo cache for %s repos", &config.Github.Repositories)
			for _, repo := range config.Github.Repositories.Value() {
				r := strings.Split(repo, "/")
				repoCache := repoCache{}
				repoCache.repo, _, err = client.Repositories.Get(context.Background(), r[0], r[1])
				if err != nil {
					log.Printf("Get error for %s: %s", repo, err.Error())
					continue
				}
				repoCache.hasWorkflows = hasWorkflows(repoCache.repo.GetFullName())
				repoCache.recheckTime = time.Now()
				Cache.Set(repo, &repoCache, gocache.NoExpiration)
			}
		} else {
			for _, org := range config.Github.Organizations.Value() {
				repos, err := listOrgRepos(org)
				if err != nil {
					log.Printf("ListByOrg error for %s: %s", org, err.Error())
					continue
				}
				for _, repo := range repos {
					Cache.Set(repo.GetFullName(), &repoCache{
						repo:         &repo,
						hasWorkflows: hasWorkflows(repo.GetFullName()),
						recheckTime:  time.Now(),
					}, cache.NoExpiration)
				}
				// remove repos that does not exists
				for repo := range Cache.Items() {
					if !containsRepo(repos, repo) {
						Cache.Delete(repo)
					}
				}
			}
		}
		log.Printf("Repo cache contains %d repos", len(Cache.Items()))
		log.Printf("Refresh repo cache in: %s", recheck)
		time.Sleep(recheck)
	}
}

func containsRepo(repos []github.Repository, repo string) bool {
	for _, r := range repos {
		if r.GetFullName() == repo {
			return true
		}
	}
	log.Printf("%s not found, deleting from cache", repo)
	return false
}

func hasWorkflows(repo string) bool {
	r := strings.Split(repo, "/")
	resp, _, err := client.Actions.ListWorkflows(context.Background(), r[0], r[1], nil)
	if err != nil {
		log.Printf("ListWorkflows error for %s: %s", repo, err.Error())
		return false
	}
	return resp.GetTotalCount() > 0
}

// workflowCache - used for limit calls to github api
func workflowCache(reCheckActive, reCheckInactive time.Duration) {
	for {
		var repos []string
		for repo, item := range Cache.Items() {
			if item.Object.(*repoCache).recheckTime.Before(time.Now()) && item.Object.(*repoCache).hasWorkflows {
				repos = append(repos, repo)
				item.Object.(*repoCache).recheckTime = time.Now().Add(reCheckActive)
			}
		}
		ww := make(map[string]map[int64]github.Workflow)
		log.Printf("Refresh workflow cache for %d repos", len(repos))
		for _, repo := range repos {
			r := strings.Split(repo, "/")
			resp, req, err := client.Actions.ListWorkflows(context.Background(), r[0], r[1], nil)
			if err != nil {
				log.Printf("ListWorkflows error for %s: %s", repo, err.Error())
			} else {
				s := make(map[int64]github.Workflow)
				for _, w := range resp.Workflows {
					s[*w.ID] = *w
				}
				log.Printf("%s: %d workflows, cached: %s", repo, len(s), req.Header.Get("X-From-Cache"))
				ww[repo] = s

			}
		}
		_, resp, err := client.APIMeta(context.Background())
		if err != nil {
			log.Printf("APIMeta error: %s", err.Error())
		} else {
			log.Printf("API rate limit: %d/%d", resp.Rate.Remaining, resp.Rate.Limit)
		}
		workflows = ww
		log.Printf("Workflow cache contains %d repos", len(workflows))
		time.Sleep(time.Duration(config.Github.Refresh) * time.Second)
	}
}
