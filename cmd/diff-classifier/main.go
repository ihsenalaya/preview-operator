// diff-classifier queries the GitHub API for a pull request diff, classifies each
// changed file by its role in the system, and outputs a JSON merge-patch fragment
// that can be applied to a Preview CR with `kubectl patch --type=merge`.
//
// Usage:
//
//	diff-classifier --repo owner/repo --pr 123 [--token $GITHUB_TOKEN] [--base-url https://api.github.com]
//
// Output (stdout):
//
//	{"spec":{"changeContext":{...}}}
//
// Example GitHub Actions step:
//
//	PATCH=$(diff-classifier --repo ${{ github.repository }} --pr ${{ github.event.pull_request.number }} --token ${{ secrets.GITHUB_TOKEN }})
//	kubectl patch preview pr-${{ github.event.pull_request.number }} --type=merge -p "$PATCH"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/pkg/changecontext/classifier"
)

type prMeta struct {
	Additions    int   `json:"additions"`
	Deletions    int   `json:"deletions"`
	ChangedFiles int   `json:"changed_files"`
	Base         psSHA `json:"base"`
	Head         psSHA `json:"head"`
}

type psSHA struct {
	SHA string `json:"sha"`
}

type prFile struct {
	Filename string `json:"filename"`
}

func main() {
	repo := flag.String("repo", "", "owner/repo (required)")
	pr := flag.Int("pr", 0, "pull request number (required)")
	token := flag.String("token", "", "GitHub token (falls back to $GITHUB_TOKEN)")
	baseURL := flag.String("base-url", "https://api.github.com", "GitHub API base URL")
	flag.Parse()

	if *token == "" {
		*token = os.Getenv("GITHUB_TOKEN")
	}
	if *repo == "" || *pr == 0 {
		fmt.Fprintln(os.Stderr, "usage: diff-classifier --repo owner/repo --pr NUMBER [--token TOKEN]")
		os.Exit(1)
	}
	parts := strings.SplitN(*repo, "/", 2)
	if len(parts) != 2 {
		fmt.Fprintln(os.Stderr, "--repo must be in owner/repo format")
		os.Exit(1)
	}
	owner, repoName := parts[0], parts[1]

	httpClient := &http.Client{Timeout: 30 * time.Second}

	get := func(url string, out interface{}) error {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if *token != "" {
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			preview := string(body)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return fmt.Errorf("GitHub API %s returned %d: %s", url, resp.StatusCode, preview)
		}
		return json.Unmarshal(body, out)
	}

	var meta prMeta
	metaURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", *baseURL, owner, repoName, *pr)
	if err := get(metaURL, &meta); err != nil {
		fmt.Fprintln(os.Stderr, "error fetching PR metadata:", err)
		os.Exit(1)
	}

	var allFiles []prFile
	for page := 1; ; page++ {
		var batch []prFile
		filesURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d",
			*baseURL, owner, repoName, *pr, page)
		if err := get(filesURL, &batch); err != nil {
			fmt.Fprintln(os.Stderr, "error fetching PR files:", err)
			os.Exit(1)
		}
		allFiles = append(allFiles, batch...)
		if len(batch) < 100 {
			break
		}
	}

	changedFiles := make([]platformv1alpha1.ChangedFile, len(allFiles))
	for i, f := range allFiles {
		changedFiles[i] = platformv1alpha1.ChangedFile{
			Path: f.Filename,
			Type: classifier.Classify(f.Filename),
		}
	}

	impacts := classifier.DetectImpacts(changedFiles)

	changeCtx := platformv1alpha1.ChangeContextSpec{
		DiffRef: platformv1alpha1.DiffRef{
			Provider:          "github",
			Repository:        *repo,
			PullRequestNumber: *pr,
			BaseSHA:           meta.Base.SHA,
			HeadSHA:           meta.Head.SHA,
		},
		Summary: platformv1alpha1.ChangeSummary{
			ChangedFilesCount: len(allFiles),
			Additions:         meta.Additions,
			Deletions:         meta.Deletions,
		},
		ChangedFiles:    changedFiles,
		DetectedImpacts: impacts,
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"changeContext": changeCtx,
		},
	}
	out, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error marshaling output:", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(out)
	_, _ = os.Stdout.WriteString("\n")
}
