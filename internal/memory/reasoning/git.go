package reasoning

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rand/recurse/internal/diff"
)

// GitInfo contains current git repository state.
type GitInfo struct {
	Branch     string
	CommitHash string
	IsDirty    bool
}

// GitIntegration provides git operations for reasoning traces.
type GitIntegration struct {
	workDir string
}

// NewGitIntegration creates a new GitIntegration for the given directory.
func NewGitIntegration(workDir string) *GitIntegration {
	return &GitIntegration{workDir: workDir}
}

// GetCurrentState returns the current git branch and commit hash.
func (g *GitIntegration) GetCurrentState(ctx context.Context) (*GitInfo, error) {
	info := &GitInfo{}

	// Get current branch
	branch, err := g.runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("get branch: %w", err)
	}
	info.Branch = strings.TrimSpace(branch)

	// Get current commit hash
	hash, err := g.runGit(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}
	info.CommitHash = strings.TrimSpace(hash)

	// Check if working directory is dirty
	status, err := g.runGit(ctx, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}
	info.IsDirty = strings.TrimSpace(status) != ""

	return info, nil
}

// GetFileAtHEAD returns the content of a file at HEAD.
func (g *GitIntegration) GetFileAtHEAD(ctx context.Context, filePath string) (string, error) {
	relPath, err := g.relativePath(filePath)
	if err != nil {
		return "", err
	}

	content, err := g.runGit(ctx, "show", "HEAD:"+relPath)
	if err != nil {
		// File might not exist at HEAD (new file)
		return "", nil
	}
	return content, nil
}

// GetFileAtCommit returns the content of a file at a specific commit.
func (g *GitIntegration) GetFileAtCommit(ctx context.Context, filePath, commitHash string) (string, error) {
	relPath, err := g.relativePath(filePath)
	if err != nil {
		return "", err
	}

	content, err := g.runGit(ctx, "show", commitHash+":"+relPath)
	if err != nil {
		return "", nil // File might not exist at that commit
	}
	return content, nil
}

// CaptureFileDiff captures the diff for a single file between HEAD and working directory.
func (g *GitIntegration) CaptureFileDiff(ctx context.Context, filePath, currentContent string) (*DiffRecord, error) {
	relPath, err := g.relativePath(filePath)
	if err != nil {
		return nil, err
	}

	// Get content at HEAD
	beforeContent, err := g.GetFileAtHEAD(ctx, filePath)
	if err != nil {
		beforeContent = "" // New file
	}

	// Generate unified diff
	unifiedDiff, additions, removals := diff.GenerateDiff(beforeContent, currentContent, relPath)

	// Get current git state
	info, _ := g.GetCurrentState(ctx) // Ignore error, commit hash is optional

	record := &DiffRecord{
		FilePath:      relPath,
		BeforeContent: beforeContent,
		AfterContent:  currentContent,
		UnifiedDiff:   unifiedDiff,
		Additions:     additions,
		Removals:      removals,
		CapturedAt:    time.Now().UTC(),
	}

	if info != nil {
		record.CommitHash = info.CommitHash
	}

	return record, nil
}

// CaptureWorkingDiffs captures diffs for all modified files in the working directory.
func (g *GitIntegration) CaptureWorkingDiffs(ctx context.Context) ([]DiffRecord, error) {
	// Get list of modified files
	output, err := g.runGit(ctx, "diff", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("get modified files: %w", err)
	}

	// Also get untracked files
	untrackedOutput, err := g.runGit(ctx, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("get untracked files: %w", err)
	}

	// Combine modified and untracked
	var files []string
	for _, f := range strings.Split(output, "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	for _, f := range strings.Split(untrackedOutput, "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}

	if len(files) == 0 {
		return nil, nil
	}

	var diffs []DiffRecord
	for _, relPath := range files {
		absPath := filepath.Join(g.workDir, relPath)

		// Read current content
		currentContent, err := readFileContent(absPath)
		if err != nil {
			continue // Skip files we can't read
		}

		record, err := g.CaptureFileDiff(ctx, absPath, currentContent)
		if err != nil {
			continue // Skip files with errors
		}

		// Only include if there are actual changes
		if record.Additions > 0 || record.Removals > 0 || record.BeforeContent == "" {
			diffs = append(diffs, *record)
		}
	}

	return diffs, nil
}

// CaptureStagedDiffs captures diffs for all staged files.
func (g *GitIntegration) CaptureStagedDiffs(ctx context.Context) ([]DiffRecord, error) {
	// Get list of staged files
	output, err := g.runGit(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("get staged files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	if len(files) == 0 || (len(files) == 1 && files[0] == "") {
		return nil, nil
	}

	var diffs []DiffRecord
	info, _ := g.GetCurrentState(ctx)

	for _, relPath := range files {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}

		// Get staged diff
		diffOutput, err := g.runGit(ctx, "diff", "--cached", "--", relPath)
		if err != nil {
			continue
		}

		// Count additions and removals
		additions, removals := countDiffStats(diffOutput)

		record := DiffRecord{
			FilePath:    relPath,
			UnifiedDiff: diffOutput,
			Additions:   additions,
			Removals:    removals,
			CapturedAt:  time.Now().UTC(),
		}

		if info != nil {
			record.CommitHash = info.CommitHash
		}

		diffs = append(diffs, record)
	}

	return diffs, nil
}

// CaptureCommitDiffs captures diffs for a specific commit.
func (g *GitIntegration) CaptureCommitDiffs(ctx context.Context, commitHash string) ([]DiffRecord, error) {
	// Get diff for the commit
	output, err := g.runGit(ctx, "show", "--format=", "--name-only", commitHash)
	if err != nil {
		return nil, fmt.Errorf("get commit files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	if len(files) == 0 {
		return nil, nil
	}

	var diffs []DiffRecord
	for _, relPath := range files {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}

		// Get the diff for this file in this commit
		diffOutput, err := g.runGit(ctx, "show", "--format=", commitHash, "--", relPath)
		if err != nil {
			continue
		}

		additions, removals := countDiffStats(diffOutput)

		diffs = append(diffs, DiffRecord{
			FilePath:    relPath,
			UnifiedDiff: diffOutput,
			Additions:   additions,
			Removals:    removals,
			CommitHash:  commitHash,
			CapturedAt:  time.Now().UTC(),
		})
	}

	return diffs, nil
}

// runGit executes a git command and returns the output.
func (g *GitIntegration) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// relativePath converts an absolute path to a path relative to the git root.
func (g *GitIntegration) relativePath(absPath string) (string, error) {
	if !filepath.IsAbs(absPath) {
		return absPath, nil // Already relative
	}

	relPath, err := filepath.Rel(g.workDir, absPath)
	if err != nil {
		return "", fmt.Errorf("relative path: %w", err)
	}
	return relPath, nil
}

// countDiffStats counts additions and removals in a unified diff.
func countDiffStats(diffOutput string) (additions, removals int) {
	for _, line := range strings.Split(diffOutput, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removals++
		}
	}
	return
}

// readFileContent reads the content of a file.
func readFileContent(path string) (string, error) {
	cmd := exec.Command("cat", path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}
