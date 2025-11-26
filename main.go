package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// PackageJSON represents the minimal structure we need from package.json
type PackageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// loadIOCs reads the IOC file and returns a map of package entries (name,version -> true)
func loadIOCs(iocPath string) (map[string]bool, error) {
	file, err := os.Open(iocPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open IOC file: %w", err)
	}
	defer file.Close()

	iocs := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse format: package-name,version
		parts := strings.Split(line, ",")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Warning: invalid format at line %d: %s\n", lineNum, line)
			continue
		}

		name := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])

		if name == "" || version == "" {
			fmt.Fprintf(os.Stderr, "Warning: empty name or version at line %d: %s\n", lineNum, line)
			continue
		}

		// Store as "name,version" key for easy lookup
		key := fmt.Sprintf("%s,%s", name, version)
		iocs[key] = true
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read IOC file: %w", err)
	}

	return iocs, nil
}

// expandEnvVars expands environment variables in a path
// Supports both %VAR% (Windows) and $VAR or ${VAR} (Unix) syntax
func expandEnvVars(path string) string {
	// First expand Unix-style variables using os.ExpandEnv
	result := os.ExpandEnv(path)

	// Then expand Windows-style %VAR% variables
	re := regexp.MustCompile(`%([^%]+)%`)
	result = re.ReplaceAllStringFunc(result, func(match string) string {
		varName := strings.Trim(match, "%")
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Keep original if not found
	})

	return result
}

// expandGlobPath expands glob patterns in a path and returns all matching paths
func expandGlobPath(path string) []string {
	// First expand environment variables
	expandedPath := expandEnvVars(path)

	// Clean the path (normalize separators)
	expandedPath = filepath.Clean(expandedPath)

	// Check if path contains glob patterns
	if !strings.Contains(expandedPath, "*") && !strings.Contains(expandedPath, "?") {
		return []string{expandedPath}
	}

	// Use filepath.Glob to expand
	matches, err := filepath.Glob(expandedPath)
	if err != nil || len(matches) == 0 {
		// Return original path if glob fails or no matches
		return []string{expandedPath}
	}

	return matches
}

// isPathForCurrentOS checks if a path is intended for the current OS
func isPathForCurrentOS(path string) bool {
	isWindows := runtime.GOOS == "windows"

	// Check for definitive OS-specific patterns
	// Leading "/" is a definitive Unix absolute path indicator
	isDefinitelyUnix := strings.HasPrefix(path, "/")

	// Drive letter (e.g., "C:") is a definitive Windows path indicator
	isDefinitelyWindows := len(path) >= 2 && path[1] == ':'

	// Definitive indicators take priority - reject paths clearly meant for other OS
	if isWindows {
		// On Windows, reject paths that definitively start with Unix root
		if isDefinitelyUnix {
			return false
		}
		// Accept Windows paths or ambiguous paths (relative, env vars only, etc.)
		return true
	}

	// On Unix, reject paths that have Windows drive letters
	if isDefinitelyWindows {
		return false
	}
	// Accept Unix paths or ambiguous paths
	return true
}

// loadPathsFromFile reads scan paths from a file
func loadPathsFromFile(pathsFile string) ([]string, error) {
	file, err := os.Open(pathsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open paths file: %w", err)
	}
	defer file.Close()

	var paths []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip paths not intended for current OS
		if !isPathForCurrentOS(line) {
			continue
		}

		// Expand glob patterns (which also expands env vars)
		expandedPaths := expandGlobPath(line)
		paths = append(paths, expandedPaths...)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read paths file: %w", err)
	}

	return paths, nil
}

// getDefaultPaths returns fallback paths if no paths file is found
func getDefaultPaths() []string {
	dirs := []string{
		"/usr/local/lib/node_modules",
		"/opt/homebrew/lib/node_modules",
	}

	// Add Homebrew Intel Cellar paths using glob expansion
	cellarPaths, err := filepath.Glob("/usr/local/Cellar/node/*/lib/node_modules")
	if err == nil {
		dirs = append(dirs, cellarPaths...)
	}

	return dirs
}

// scanDirectory recursively walks a directory and checks for IOC matches
func scanDirectory(dirPath string, iocs map[string]bool) ([]string, error) {
	var matches []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories that we can't access
			return nil
		}

		// Look for package.json files in node_modules
		if info.IsDir() || info.Name() != "package.json" {
			return nil
		}

		// Check if this is in a node_modules directory
		if !strings.Contains(path, "node_modules") {
			return nil
		}

		// Read and parse package.json
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			return nil
		}

		var pkg PackageJSON
		if err := json.Unmarshal(data, &pkg); err != nil {
			return nil
		}

		// Check if package name and version matches any IOC
		if pkg.Name != "" && pkg.Version != "" {
			key := fmt.Sprintf("%s,%s", pkg.Name, pkg.Version)
			if iocs[key] {
				packageDir := filepath.Dir(path)
				matches = append(matches, fmt.Sprintf("[MATCH] %s@%s: %s", pkg.Name, pkg.Version, packageDir))
			}
		}

		return nil
	})

	if err != nil {
		return matches, err
	}

	return matches, nil
}

func main() {
	// Define command-line flags
	iocPath := flag.String("ioc", "ioc.txt", "Path to IOC file")
	pathsFile := flag.String("paths", "paths.txt", "Path to file containing scan paths")
	scanGlobal := flag.Bool("global", true, "Scan paths from paths file (or default paths if file not found)")
	flag.Parse()

	fmt.Println("Exit codes: 0 = no matches found, 1 = matches found, 2 = no scan due to misconfiguration, -1 = error")

	// Load IOCs
	iocs, err := loadIOCs(*iocPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading IOCs: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("Loaded %d IOCs from %s\n", len(iocs), *iocPath)

	// Collect directories to scan
	var dirsToScan []string

	// Add directories from paths file if requested
	if *scanGlobal {
		paths, err := loadPathsFromFile(*pathsFile)
		if err != nil {
			fmt.Printf("Warning: Could not load paths from %s: %v\n", *pathsFile, err)
			fmt.Println("Using default paths...")
			dirsToScan = append(dirsToScan, getDefaultPaths()...)
		} else {
			fmt.Printf("Loaded %d paths from %s\n", len(paths), *pathsFile)
			dirsToScan = append(dirsToScan, paths...)
		}
	}

	// Add additional directories from command-line arguments
	additionalPaths := flag.Args()
	for _, p := range additionalPaths {
		expanded := expandGlobPath(p)
		dirsToScan = append(dirsToScan, expanded...)
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var uniqueDirs []string
	for _, dir := range dirsToScan {
		if !seen[dir] {
			seen[dir] = true
			uniqueDirs = append(uniqueDirs, dir)
		}
	}
	dirsToScan = uniqueDirs

	if len(dirsToScan) == 0 {
		fmt.Println("No directories to scan. Use -global flag or provide paths as arguments.")
		os.Exit(2)
	}

	// Scan each directory
	var allMatches []string
	for _, dir := range dirsToScan {
		// Check if directory exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			fmt.Printf("Skipping non-existent directory: %s\n", dir)
			continue
		}

		fmt.Printf("Scanning: %s\n", dir)
		matches, err := scanDirectory(dir, iocs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error scanning %s: %v\n", dir, err)
		}
		allMatches = append(allMatches, matches...)
	}

	// Report results
	fmt.Printf("\nScan complete. Found %d matches.\n", len(allMatches))
	if len(allMatches) > 0 {
		fmt.Println("\nMatches:")
		for _, match := range allMatches {
			fmt.Println(match)
		}
		os.Exit(1)
	}
	os.Exit(0)
}
