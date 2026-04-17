package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/pflag"
)

// ============================================================
//  Configuration & Data Types
// ============================================================

const versionStr = "v3.2.0"

type Config struct {
	RootDir           string
	OutputFile        string
	IncludeExts       []string
	IncludeMatches    []string
	ExcludeExts       []string
	ExcludeMatches    []string
	MaxFileSize       int64
	NoSubdirs         bool
	Verbose           bool
	Version           bool
	ShowStats         bool
	DryRun            bool
	NoDefaultIgnore   bool
	NoGitignore       bool
	AdditionalIgnores []string
}

type FileMetadata struct {
	RelPath   string
	FullPath  string
	Size      int64
	LineCount int
}

type Stats struct {
	PotentialMatches   int
	ExplicitlyExcluded int
	FileCount          int
	TotalSize          int64
	TotalLines         int
	Skipped            int
	DirCount           int
}

type SkippedFile struct {
	RelPath string
	Reason  string
}

type DirStats struct {
	Path       string
	FileCount  int
	TotalSize  int64
	TotalLines int
}

type ExtStats struct {
	Ext        string
	FileCount  int
	TotalSize  int64
	TotalLines int
}

// ============================================================
//  Ignore Rules
// ============================================================

var ignoreDirs = map[string]bool{
	".git": true, ".idea": true, ".vscode": true, ".svn": true, ".hg": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "bin": true, "out": true, "release": true, "debug": true,
	"__pycache__": true, ".pytest_cache": true, ".tox": true,
	".env": true, ".venv": true, "venv": true, "env": true,
	"Pods": true, "Carthage": true, "CocoaPods": true,
	"obj": true, "ipch": true, "Debug": true, "Release": true,
	"x64": true, "x86": true, "arm64": true,
	".gradle": true, ".sonar": true, ".scannerwork": true,
	"logs": true, "tmp": true, "temp": true, "cache": true,
	".history": true, ".nyc_output": true, ".coverage": true,
}

var ignoreFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "go.sum": true,
	"composer.lock": true, "Gemfile.lock": true,
	"tags": true, "TAGS": true, ".DS_Store": true,
	"coverage.xml": true, "thumbs.db": true,
}

var languageMap = map[string]string{
	".go": "go", ".js": "javascript", ".ts": "typescript", ".py": "python",
	".c": "c", ".cpp": "cpp", ".h": "cpp", ".hpp": "cpp", ".cc": "cpp",
	".java": "java", ".rb": "ruby", ".php": "php", ".rs": "rust",
	".swift": "swift", ".kt": "kotlin", ".m": "objectivec", ".mm": "objectivec",
	".sh": "bash", ".zsh": "bash", ".bash": "bash", ".fish": "fish",
	".yml": "yaml", ".yaml": "yaml", ".json": "json", ".xml": "xml",
	".html": "html", ".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
	".md": "markdown", ".sql": "sql", ".graphql": "graphql", ".proto": "protobuf",
	".dockerfile": "dockerfile", ".makefile": "makefile", ".cmake": "cmake",
	".vue": "vue", ".svelte": "svelte", ".dart": "dart", ".lua": "lua",
	".pl": "perl", ".ex": "elixir", ".erl": "erlang", ".hs": "haskell",
	".ml": "ocaml", ".clj": "clojure", ".tf": "hcl",
}

// ============================================================
//  Main Entry
// ============================================================

func main() {
	config := parseFlags()

	if config.Version {
		fmt.Printf("gdox %s\n", versionStr)
		return
	}

	if config.Verbose {
		fmt.Printf("▶ Root directory: %s\n", config.RootDir)
		fmt.Printf("▶ Output file: %s\n", config.OutputFile)
	}

	if !config.DryRun {
		fmt.Println("▶ Gdox Started")
	} else {
		fmt.Println("▶ Gdox Dry-Run Mode")
	}

	startTime := time.Now()

	// 1. Scan and filter
	files, stats, skipped := scanDirectory(config)

	if config.DryRun {
		printDryRun(files, stats, skipped)
		return
	}

	// 2. Generate content
	err := generateOutput(config, files, stats)
	if err != nil {
		fmt.Printf("❌ Error generating output: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✨ Done! Generated %s in %v\n", config.OutputFile, duration)
	fmt.Printf("📊 Files processed: %d | Total lines: %d | Total size: %.2f KB\n",
		stats.FileCount, stats.TotalLines, float64(stats.TotalSize)/1024)
}

func parseFlags() Config {
	var c Config
	pflag.StringVarP(&c.RootDir, "dir", "d", ".", "Root directory to scan")
	pflag.StringVarP(&c.OutputFile, "out", "o", "project_snapshot.md", "Output markdown file")
	
	var incExts, incMatches, excExts, excMatches, addIgnores string
	pflag.StringVarP(&incExts, "include", "i", "", "Include extensions (comma separated, e.g. go,js)")
	pflag.StringVarP(&incMatches, "match", "m", "", "Include path keywords (comma separated, e.g. _test.go)")
	pflag.StringVarP(&excExts, "exclude", "x", "", "Exclude extensions (comma separated)")
	pflag.StringVarP(&excMatches, "exclude-match", "X", "", "Exclude path keywords (comma separated, e.g. vendor/)")
	pflag.StringVarP(&addIgnores, "ignore", "", "", "Additional ignore patterns (comma separated)")

	pflag.Int64Var(&c.MaxFileSize, "max-size", 500, "Max file size in KB")
	pflag.BoolVarP(&c.NoSubdirs, "no-subdirs", "n", false, "Do not scan subdirectories")
	pflag.BoolVarP(&c.Verbose, "verbose", "v", false, "Verbose output")
	pflag.BoolVar(&c.Version, "version", false, "Show version")
	pflag.BoolVarP(&c.ShowStats, "stats", "s", false, "Show detailed statistics")
	pflag.BoolVar(&c.DryRun, "dry-run", false, "Dry run mode (no file write)")
	pflag.BoolVar(&c.NoDefaultIgnore, "no-default-ignore", false, "Disable default ignore rules")
	pflag.BoolVar(&c.NoGitignore, "no-gitignore", false, "Do not load .gitignore")

	pflag.Parse()

	if incExts != "" {
		c.IncludeExts = cleanList(incExts)
	}
	if incMatches != "" {
		c.IncludeMatches = cleanList(incMatches)
	}
	if excExts != "" {
		c.ExcludeExts = cleanList(excExts)
	}
	if excMatches != "" {
		c.ExcludeMatches = cleanList(excMatches)
	}
	if addIgnores != "" {
		c.AdditionalIgnores = cleanList(addIgnores)
	}

	c.MaxFileSize *= 1024 // KB to Bytes
	absRoot, _ := filepath.Abs(c.RootDir)
	c.RootDir = absRoot

	return c
}

func cleanList(s string) []string {
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			if !strings.HasPrefix(trimmed, ".") && len(trimmed) > 0 && !strings.Contains(trimmed, "/") && !strings.Contains(trimmed, "\\") {
				// Likely an extension without dot
				res = append(res, "."+trimmed)
			} else {
				res = append(res, trimmed)
			}
		}
	}
	return res
}

// ============================================================
//  Scanning Logic
// ============================================================

func scanDirectory(config Config) ([]FileMetadata, Stats, []SkippedFile) {
	var files []FileMetadata
	var stats Stats
	var skipped []SkippedFile

	gitignorePatterns := []string{}
	if !config.NoGitignore {
		gitignorePatterns = loadGitignore(config.RootDir)
	}

	err := filepath.WalkDir(config.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if config.Verbose {
				fmt.Printf("⚠️ Error accessing %s: %v\n", path, err)
			}
			return nil
		}

		relPath, _ := filepath.Rel(config.RootDir, path)
		if relPath == "." {
			return nil
		}

		// 1. Directory Checks
		if d.IsDir() {
			if shouldIgnoreDir(relPath, config, gitignorePatterns) {
				if config.Verbose {
					fmt.Printf("⏭ Ignoring directory: %s\n", relPath)
				}
				return filepath.SkipDir
			}
			if config.NoSubdirs && relPath != "." && strings.Contains(relPath, string(filepath.Separator)) {
				return filepath.SkipDir
			}
			stats.DirCount++
			return nil
		}

		// 2. File Checks
		stats.PotentialMatches++

		if shouldIgnoreFile(relPath, config, gitignorePatterns) {
			stats.ExplicitlyExcluded++
			return nil
		}

		info, _ := d.Info()
		if info.Size() > config.MaxFileSize {
			skipped = append(skipped, SkippedFile{relPath, fmt.Sprintf("Size exceeds %d KB", config.MaxFileSize/1024)})
			stats.Skipped++
			return nil
		}

		if isBinaryFile(path) {
			skipped = append(skipped, SkippedFile{relPath, "Binary file"})
			stats.Skipped++
			return nil
		}

		// 3. Metadata Collection
		lineCount := countLines(path)
		files = append(files, FileMetadata{
			RelPath:   relPath,
			FullPath:  path,
			Size:      info.Size(),
			LineCount: lineCount,
		})

		stats.FileCount++
		stats.TotalSize += info.Size()
		stats.TotalLines += lineCount

		return nil
	})

	if err != nil && config.Verbose {
		fmt.Printf("❌ Walk error: %v\n", err)
	}

	return files, stats, skipped
}

func shouldIgnoreDir(relPath string, config Config, gitPatterns []string) bool {
	name := filepath.Base(relPath)
	
	if !config.NoDefaultIgnore {
		if ignoreDirs[name] {
			return true
		}
	}

	for _, pattern := range config.AdditionalIgnores {
		if matchPattern(relPath, pattern) {
			return true
		}
	}

	for _, pattern := range gitPatterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}

	return false
}

func shouldIgnoreFile(relPath string, config Config, gitPatterns []string) bool {
	name := filepath.Base(relPath)
	ext := filepath.Ext(relPath)

	// Default ignores
	if !config.NoDefaultIgnore {
		if ignoreFiles[name] {
			return true
		}
	}

	// Extension includes
	if len(config.IncludeExts) > 0 {
		found := false
		for _, e := range config.IncludeExts {
			if strings.EqualFold(ext, e) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}

	// Path keyword includes
	if len(config.IncludeMatches) > 0 {
		found := false
		for _, m := range config.IncludeMatches {
			if strings.Contains(relPath, m) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}

	// Extension excludes
	for _, e := range config.ExcludeExts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}

	// Path keyword excludes
	for _, m := range config.ExcludeMatches {
		if strings.Contains(relPath, m) {
			return true
		}
	}

	// Custom ignores
	for _, pattern := range config.AdditionalIgnores {
		if matchPattern(relPath, pattern) {
			return true
		}
	}

	// Gitignore
	for _, pattern := range gitPatterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}

	return false
}

func loadGitignore(root string) []string {
	var patterns []string
	gitPath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return patterns
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func matchPattern(path, pattern string) bool {
	// Simple glob matching
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	if matched {
		return true
	}
	// Simple path prefix matching
	if strings.HasPrefix(path, pattern) {
		return true
	}
	return false
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buffer := make([]byte, 1024)
	n, err := f.Read(buffer)
	if err != nil && err != io.EOF {
		return true
	}

	if n == 0 {
		return false
	}

	if !utf8.Valid(buffer[:n]) {
		// Check for common non-text bytes
		for _, b := range buffer[:n] {
			if b == 0 {
				return true
			}
		}
	}

	return false
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}

// ============================================================
//  Output Generation
// ============================================================

func generateOutput(config Config, files []FileMetadata, stats Stats) error {
	out, err := os.Create(config.OutputFile)
	if err != nil {
		return err
	}
	defer out.Close()

	writer := bufio.NewWriter(out)
	defer writer.Flush()

	// 1. Header
	fmt.Fprintf(writer, "# Project Snapshot: %s\n\n", filepath.Base(config.RootDir))
	fmt.Fprintf(writer, "> Generated by [Gdox](https://github.com/yuanguangshan/gdox) on %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// 2. TOC
	fmt.Fprintln(writer, "## Table of Contents")
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	for _, f := range files {
		anchor := generateAnchor(f.RelPath)
		fmt.Fprintf(writer, "- [%s](#%s)\n", f.RelPath, anchor)
	}
	fmt.Fprintln(writer, "\n---")

	// 3. File Contents
	for _, f := range files {
		fmt.Fprintf(writer, "\n## %s\n\n", f.RelPath)
		
		content, err := os.ReadFile(f.FullPath)
		if err != nil {
			fmt.Fprintf(writer, "> ❌ Error reading file: %v\n", err)
			continue
		}

		lang := detectLanguage(f.RelPath)
		fence := determineFence(string(content))
		
		fmt.Fprintf(writer, "%s%s\n", fence, lang)
		writer.Write(content)
		if !bytes.HasSuffix(content, []byte("\n")) {
			writer.WriteByte('\n')
		}
		fmt.Fprintf(writer, "%s\n", fence)
	}

	// 4. Footer/Stats
	if config.ShowStats {
		fmt.Fprintln(writer, "\n---")
		fmt.Fprintln(writer, "## Project Statistics")
		fmt.Fprintf(writer, "- **Files Processed**: %d\n", stats.FileCount)
		fmt.Fprintf(writer, "- **Total Lines**: %d\n", stats.TotalLines)
		fmt.Fprintf(writer, "- **Total Size**: %.2f KB\n", float64(stats.TotalSize)/1024)
		fmt.Fprintf(writer, "- **Directories**: %d\n", stats.DirCount)
	}

	return nil
}

func generateAnchor(path string) string {
	a := strings.ToLower(path)
	a = strings.ReplaceAll(a, "/", "-")
	a = strings.ReplaceAll(a, "\\", "-")
	a = strings.ReplaceAll(a, ".", "-")
	return a
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := languageMap[ext]; ok {
		return lang
	}
	// Check for files without extensions
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" {
		return "dockerfile"
	}
	if base == "makefile" {
		return "makefile"
	}
	return ""
}

func determineFence(content string) string {
	max := 0
	current := 0
	for _, r := range content {
		if r == '`' {
			current++
			if current > max {
				max = current
			}
		} else {
			current = 0
		}
	}
	if max < 3 {
		return "```"
	}
	return strings.Repeat("`", max+1)
}

func printDryRun(files []FileMetadata, stats Stats, skipped []SkippedFile) {
	fmt.Printf("\n🔍 [Dry-Run] Files to be included (%d):\n", len(files))
	for _, f := range files {
		fmt.Printf("  - %s (%d lines, %.2f KB)\n", f.RelPath, f.LineCount, float64(f.Size)/1024)
	}

	if len(skipped) > 0 {
		fmt.Printf("\n⏭  Skipped files (%d):\n", len(skipped))
		for _, s := range skipped {
			fmt.Printf("  - %s [%s]\n", s.RelPath, s.Reason)
		}
	}

	fmt.Printf("\n📊 Potential files: %d | Excluded: %d | Final: %d\n",
		stats.PotentialMatches, stats.ExplicitlyExcluded, stats.FileCount)
}
