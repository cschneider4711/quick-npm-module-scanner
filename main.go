package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// getGlobalNPMDirectories returns the default macOS global npm directories
func getGlobalNPMDirectories() []string {
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
	scanGlobal := flag.Bool("global", true, "Scan global npm directories")
	flag.Parse()

	// Load IOCs
	iocs, err := loadIOCs(*iocPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading IOCs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded %d IOCs from %s\n", len(iocs), *iocPath)

	// Collect directories to scan
	var dirsToScan []string

	// Add global directories if requested
	if *scanGlobal {
		dirsToScan = append(dirsToScan, getGlobalNPMDirectories()...)
	}

	// Add additional directories from command-line arguments
	additionalPaths := flag.Args()
	dirsToScan = append(dirsToScan, additionalPaths...)

	if len(dirsToScan) == 0 {
		fmt.Println("No directories to scan. Use -global flag or provide paths as arguments.")
		os.Exit(0)
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
	}
}
