package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repoFileName = "repos.json"

type RepoStore struct {
	path string
}

func NewRepoStore() (*RepoStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "pr-view")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &RepoStore{path: filepath.Join(dir, repoFileName)}, nil
}

func (s *RepoStore) Load() ([]string, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	var repos []string
	if err := json.NewDecoder(f).Decode(&repos); err != nil {
		if errors.Is(err, io.EOF) {
			return []string{}, nil
		}
		return nil, err
	}
	return repos, nil
}

func (s *RepoStore) Save(repos []string) error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(repos)
}

func (s *RepoStore) Add(repo string) error {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return fmt.Errorf("empty repo")
	}
	// accept GitHub URLs and normalize them to owner/repo or owner/repo#number
	if strings.Contains(repo, "github.com/") || strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		if u, err := url.Parse(repo); err == nil {
			path := strings.Trim(u.Path, "/")
			parts := strings.Split(path, "/")
			if len(parts) >= 4 && parts[2] == "pull" {
				// owner/repo/pull/NUMBER[/...]
				if _, err := strconv.Atoi(parts[3]); err == nil {
					repo = fmt.Sprintf("%s/%s#%s", parts[0], parts[1], parts[3])
				}
			} else if len(parts) >= 2 {
				// owner/repo or github.com/owner/repo
				repo = fmt.Sprintf("%s/%s", parts[0], parts[1])
			}
		}
	}
	// support owner/repo or owner/repo#number
	repoPart := repo
	if strings.Contains(repo, "#") {
		parts := strings.SplitN(repo, "#", 2)
		repoPart = strings.TrimSpace(parts[0])
		numStr := strings.TrimSpace(parts[1])
		if repoPart == "" || numStr == "" {
			return fmt.Errorf("invalid format, expected owner/repo or owner/repo#number")
		}
		if _, err := strconv.Atoi(numStr); err != nil {
			return fmt.Errorf("invalid pull request number: %s", numStr)
		}
	}
	if !strings.Contains(repoPart, "/") {
		return fmt.Errorf("repo must be in owner/repo format")
	}
	repos, err := s.Load()
	if err != nil {
		return err
	}
	for _, r := range repos {
		if strings.EqualFold(r, repo) {
			return fmt.Errorf("repo already exists")
		}
	}
	repos = append(repos, repo)
	return s.Save(repos)
}

func (s *RepoStore) Remove(repo string) error {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return fmt.Errorf("empty repo")
	}
	repos, err := s.Load()
	if err != nil {
		return err
	}
	idx := -1
	for i, r := range repos {
		if strings.EqualFold(r, repo) || r == repo {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("repo not found")
	}
	repos = append(repos[:idx], repos[idx+1:]...)
	return s.Save(repos)
}

type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	HTMLURL   string    `json:"html_url"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	Login string `json:"login"`
}

type PRResult struct {
	Repo string
	PRs  []PullRequest
	Err  error
}

func fetchPRs(repo string, token string) ([]PullRequest, error) {
	// repo may be owner/repo or owner/repo#number
	repoPart := repo
	var singlePR bool
	var prNumber int
	if strings.Contains(repo, "#") {
		p := strings.SplitN(repo, "#", 2)
		repoPart = strings.TrimSpace(p[0])
		nstr := strings.TrimSpace(p[1])
		if nstr != "" {
			if n, err := strconv.Atoi(nstr); err == nil {
				singlePR = true
				prNumber = n
			}
		}
	}
	parts := strings.SplitN(repoPart, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo: %s", repo)
	}
	owner, name := parts[0], parts[1]
	var url string
	if singlePR {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, name, prNumber)
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=open", owner, name)
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(resp.Body)
	if singlePR {
		var pr PullRequest
		if err := dec.Decode(&pr); err != nil {
			return nil, err
		}
		return []PullRequest{pr}, nil
	}
	var prs []PullRequest
	if err := dec.Decode(&prs); err != nil {
		return nil, err
	}
	return prs, nil
}

func cmdAdd(args []string) int {
	if len(args) < 1 {
		fmt.Println("usage: pr-view add owner/repo[#number]")
		return 2
	}
	store, err := NewRepoStore()
	if err != nil {
		fmt.Println("error initializing store:", err)
		return 1
	}
	repo := args[0]
	if err := store.Add(repo); err != nil {
		fmt.Println("error adding repo:", err)
		return 1
	}
	fmt.Println("added", repo)
	return 0
}

func cmdRemove(args []string) int {
	if len(args) < 1 {
		fmt.Println("usage: pr-view remove owner/repo")
		return 2
	}
	store, err := NewRepoStore()
	if err != nil {
		fmt.Println("error initializing store:", err)
		return 1
	}
	repo := args[0]
	if err := store.Remove(repo); err != nil {
		fmt.Println("error removing repo:", err)
		return 1
	}
	fmt.Println("removed", repo)
	return 0
}

func cmdList() int {
	store, err := NewRepoStore()
	if err != nil {
		fmt.Println("error initializing store:", err)
		return 1
	}
	repos, err := store.Load()
	if err != nil {
		fmt.Println("error loading repos:", err)
		return 1
	}
	if len(repos) == 0 {
		fmt.Println("no repos configured. add one with: pr-view add owner/repo[#number]")
		return 0
	}
	token := os.Getenv("GITHUB_TOKEN")
	var wg sync.WaitGroup
	ch := make(chan PRResult, len(repos))
	for _, r := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			prs, err := fetchPRs(repo, token)
			ch <- PRResult{Repo: repo, PRs: prs, Err: err}
		}(r)
	}
	wg.Wait()
	close(ch)
	var results []PRResult
	for res := range ch {
		results = append(results, res)
	}
	printTable(results)
	return 0
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	// simple rune-safe truncation
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 3 {
		return string(rs[:max])
	}
	return string(rs[:max-3]) + "..."
}

func printTable(results []PRResult) {
	// columns: Repo, PR, Title, Author, URL
	rows := make([][3]string, 0)
	for _, res := range results {
		if res.Err != nil {
			rows = append(rows, [3]string{res.Repo, "", "(error: " + res.Err.Error() + ")"})
			continue
		}
		if len(res.PRs) == 0 {
			rows = append(rows, [3]string{res.Repo, "", "(no open PRs)"})
			continue
		}
		for _, pr := range res.PRs {
			rows = append(rows, [3]string{res.Repo, pr.HTMLURL, truncate(pr.Title, 60)})
		}
	}

	// compute widths
	widths := [3]int{4, 3, 5} // initial min widths
	for _, r := range rows {
		for i := 0; i < 3; i++ {
			l := len([]rune(r[i]))
			if l > widths[i] {
				widths[i] = l
			}
		}
	}

	// header
	hdr := [3]string{"REPO", "URL", "TITLE"}
	fmtStr := fmt.Sprintf("%%-%dv  %%-%dv  %%-%dv \n", widths[0], widths[1], widths[2])
	fmt.Printf(fmtStr, hdr[0], hdr[1], hdr[2])

	// separator
	sep := ""
	for i := 0; i < 3; i++ {
		sep += strings.Repeat("-", widths[i])
		if i < 4 {
			sep += "  "
		}
	}
	fmt.Println(sep)

	for _, r := range rows {
		fmt.Printf(fmtStr, r[0], r[1], r[2])
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: pr-view <add|remove|list>")
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var code int
	switch cmd {
	case "add":
		code = cmdAdd(args)
	case "list":
		code = cmdList()
	case "remove":
		code = cmdRemove(args)
	default:
		fmt.Println("unknown command:", cmd)
		fmt.Println("usage: pr-view <add|remove|list>")
		code = 2
	}
	os.Exit(code)
}
