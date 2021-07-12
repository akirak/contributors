package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"

	"github.com/bmatcuk/doublestar"
	"github.com/urfave/cli/v2"
)

type Config struct {
	Name      string
	Root      string
	Listen    string
	Threshold int
}

func makeAbsolute(pathArg string) (string, error) {
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return pathArg, fmt.Errorf("Error from Getwd: %v", cwdErr)
	}

	if path.IsAbs(pathArg) {
		return pathArg, nil
	} else {
		return path.Join(cwd, pathArg), nil
	}
}

func verifyConfig(config *Config) error {
	rootInfo, rootErr := os.Stat(config.Root)
	if rootErr != nil {
		return fmt.Errorf("Root error: %v", rootErr)
	} else if !rootInfo.IsDir() {
		return fmt.Errorf("Root error: %s is not a directory", config.Root)
	}
	return nil
}

func stripDomain(email string) (string, error) {
	re, reError := regexp.Compile("^.+@")
	if reError != nil {
		return email, reError
	}
	return re.FindString(email), nil
}

type RepoContents map[string][]string

func runLinguist(root string) (RepoContents, error) {
	cmd := exec.Command("linguist", root, "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	if err != nil {
		return nil, errors.New("Linguist failed")
	}

	var result RepoContents
	json.Unmarshal(out, &result)
	return result, err
}

type FileList []string

type Contribution struct {
	Email      string
	Nlines     int
	Percentage float64
}

type Contributions []Contribution

func (a Contributions) Len() int           { return len(a) }
func (a Contributions) Less(i, j int) bool { return a[i].Nlines > a[j].Nlines }
func (a Contributions) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type LanguageStat struct {
	Language      string
	Files         []string
	TotalLines    int
	Contributions []Contribution
}

type LanguageStats []LanguageStat

func (a LanguageStats) Len() int           { return len(a) }
func (a LanguageStats) Less(i, j int) bool { return a[i].TotalLines > a[j].TotalLines }
func (a LanguageStats) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type GlobPattern = string

func getIgnoreSettings(root string) ([]GlobPattern, error) {
	ignoreFile := path.Join(root, ".contribignore")
	_, statErr := os.Stat(ignoreFile)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			var result []string
			return result, nil
		}
		return nil, statErr
	}
	file, fileErr := os.Open(ignoreFile)
	if fileErr != nil {
		return nil, fileErr
	}

	re, reErr := regexp.Compile(`^(\s*#|\s+$)`)
	if reErr != nil {
		return nil, reErr
	}

	scanner := bufio.NewScanner(file)
	var result []string
	for scanner.Scan() {
		line := scanner.Text()
		if !re.MatchString(line) {
			result = append(result, line)
		}
	}

	scannerErr := scanner.Err()
	if scannerErr != nil {
		return nil, fmt.Errorf("Error from scanner: %v", scannerErr)
	}

	return result, nil
}

func excludeWithGlob(ignorePatterns []string, files []string) ([]string, error) {
	var result []string

	for i := range files {
		file := files[i]
		var ignored bool = false
		for j := range ignorePatterns {
			x, matchErr := doublestar.Match(ignorePatterns[j], file)
			if matchErr != nil {
				return nil, fmt.Errorf("Error while matching %s on %s: %v",
					ignorePatterns[j], file,
					matchErr)
			}
			if x {
				ignored = true
				break
			}
		}
		if !ignored {
			result = append(result, file)
		}
	}
	return result, nil
}

func getContributions(root string, files []string) ([]Contribution, int, error) {
	re, reErr := regexp.Compile(`^author-mail <(.+)>`)
	if reErr != nil {
		return nil, 0, fmt.Errorf("Regexp compile error: %v", reErr)
	}

	m := make(map[string]int)
	var totalLines int
	totalLines = 0
	for i := range files {
		file := files[i]

		cmd := exec.Command("git", "blame", "--line-porcelain", "HEAD", "--", file)
		cmd.Dir = root

		stdout, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			return nil, 0, pipeErr
		}

		cmd.Start()

		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			line := scanner.Text()
			matched := re.MatchString(line)
			if matched {
				matches := re.FindStringSubmatch(line)
				if len(matches) < 1 {
					return nil, 0, fmt.Errorf("No submatch on %s", line)
				}
				author := matches[1]
				n, ok := m[author]
				if !ok {
					m[author] = 1
				} else {
					m[author] = n + 1
				}
				totalLines++
			}
		}

		scannerErr := scanner.Err()
		if scannerErr != nil {
			return nil, 0, fmt.Errorf("Error from scanner: %v", scannerErr)
		}
	}
	var result []Contribution
	for email, nlines := range m {
		result = append(result, Contribution{
			Email:      email,
			Nlines:     nlines,
			Percentage: float64(nlines) / float64(totalLines) * 100,
		})
	}
	sort.Sort(Contributions(result))
	return result, totalLines, nil
}

type Result struct {
	Contents      RepoContents
	LanguageStats []LanguageStat
}

func getStats(root string, contents *RepoContents) ([]LanguageStat, error) {
	var result []LanguageStat

	ignorePatterns, ignoreFileErr := getIgnoreSettings(root)
	if ignoreFileErr != nil {
		return nil, fmt.Errorf("Error from the ignore file: %v", ignoreFileErr)
	}

	for language, unfilteredFiles := range *contents {
		files, filterErr := excludeWithGlob(ignorePatterns, unfilteredFiles)
		if filterErr != nil {
			return nil, filterErr
		}
		contributions, totalLines, err := getContributions(root, files)
		if err != nil {
			return nil, fmt.Errorf("Error while analysing %s: %v", language, err)
		}
		result = append(result, LanguageStat{
			Language:      language,
			Files:         files,
			TotalLines:    totalLines,
			Contributions: contributions,
		})
	}
	sort.Sort(LanguageStats(result))
	return result, nil
}

func formatPercent(percent float64) string {
	if percent < 10 {
		return fmt.Sprintf("%.2f", percent)
	} else {
		return fmt.Sprintf("%.1f", percent)
	}
}

func languageProfile(result *Result, w http.ResponseWriter) {
	stats := result.LanguageStats
	fmt.Fprintln(w, "<table>")
	fmt.Fprintln(w, "<thead><tr>")
	fmt.Fprint(w, "<th>Language</th>")
	fmt.Fprint(w, "<th>Files</th>")
	fmt.Fprint(w, "<th>Lines</th>")
	fmt.Fprint(w, "<th>Percentage</th>")
	fmt.Fprint(w, "</tr>")
	fmt.Fprintln(w, "<tbody>")
	var totalLines int = 0
	for i := range stats {
		totalLines += stats[i].TotalLines
	}
	for i := range stats {
		stat := stats[i]
		fmt.Fprintf(w, "<tr>")
		fmt.Fprintf(w, "<td>%s</td>", stat.Language)
		fmt.Fprintf(w, "<td>%d</td>", len(stat.Files))
		fmt.Fprintf(w, "<td>%d</td>", stat.TotalLines)
		percentage := float64(stat.TotalLines) / float64(totalLines) * 100
		fmt.Fprintf(w, "<td>%s%%</td>", formatPercent(percentage))
		fmt.Fprintln(w, "</tr>")
	}
	fmt.Fprintln(w, "</tbody>")
	fmt.Fprintln(w, "</table>")
}

func peopleProfile(result *Result, w http.ResponseWriter) {
	m := make(map[string]int)

	stats := result.LanguageStats
	var totalLines int
	totalLines = 0
	for i := range stats {
		totalLines += stats[i].TotalLines
		contributions := stats[i].Contributions
		for j := range contributions {
			contribution := contributions[j]
			author := contribution.Email
			nlines := contribution.Nlines
			n, ok := m[author]
			if !ok {
				m[author] = nlines
			} else {
				m[author] = n + nlines
			}
		}
	}

	var contributions []Contribution
	for email, nlines := range m {
		contributions = append(contributions, Contribution{
			Email:      email,
			Nlines:     nlines,
			Percentage: float64(nlines) / float64(totalLines) * 100,
		})
	}
	sort.Sort(Contributions(contributions))

	fmt.Fprintln(w, "<table>")
	fmt.Fprintln(w, "<thead><tr>")
	fmt.Fprint(w, "<th>Person</th>")
	fmt.Fprint(w, "<th># lines</th>")
	fmt.Fprint(w, "<th>%</th>")
	fmt.Fprint(w, "</tr>")
	fmt.Fprintln(w, "<tbody>")
	var remainingPercentage float64 = 100
	var remainingContributors int = 0
	numContributors := len(contributions)
	for i := range contributions {
		c := contributions[i]
		identity, _ := stripDomain(c.Email)
		percentage := c.Percentage
		nlines := c.Nlines
		// TODO: Make this threshold customizable
		if nlines < 50 && percentage < 10 && i < numContributors-1 {
			remainingContributors = numContributors - i
			break
		}
		remainingPercentage -= percentage
		fmt.Fprint(w, "<tr>")
		fmt.Fprintf(w, "<td><span title=\"%s\">%s</span></td>", c.Email, identity)
		fmt.Fprintf(w, "<td>%d</td>", nlines)
		fmt.Fprintf(w, "<td>%s%%</td>", formatPercent(percentage))
		fmt.Fprintln(w, "</tr>")
	}
	if remainingContributors > 0 {
		fmt.Fprint(w, "<tr>")
		fmt.Fprintf(w, "<td>%d others</td>", remainingContributors)
		fmt.Fprint(w, "<td>-</td>")
		fmt.Fprintf(w, "<td>%s%%</td>", formatPercent(remainingPercentage))
		fmt.Fprintln(w, "</tr>")
	}
	fmt.Fprintln(w, "</tbody>")
	fmt.Fprintln(w, "</table>")
}

func languageStat(config *Config, stat *LanguageStat, w http.ResponseWriter) {
	fmt.Fprintf(w, "<h3>%s</h3>\n", stat.Language)

	fmt.Fprintln(w, "<details>")
	fmt.Fprintln(w, "<summary>Files</summary>")
	fmt.Fprintln(w, "<ul>")
	for j := range stat.Files {
		fmt.Fprintf(w, "<li>%s</li>", stat.Files[j])
	}
	fmt.Fprintln(w, "</ul>")
	fmt.Fprintln(w, "</details>")

	fmt.Fprintln(w, "<table>")
	fmt.Fprintln(w, "<caption>Contributors</caption>")
	fmt.Fprintln(w, "<thead>")
	fmt.Fprintln(w, "<tr>")
	fmt.Fprintln(w, "<th>E-mail</th>")
	fmt.Fprintln(w, "<th>Lines</th>")
	fmt.Fprintln(w, "<th>Percent</th>")
	fmt.Fprintln(w, "</tr>")
	fmt.Fprintln(w, "</thead>")
	fmt.Fprintln(w, "<tbody>")
	var remainingPercentage float64 = 100
	var remainingContributors int = 0
	numContributors := len(stat.Contributions)
	for j := range stat.Contributions {
		c := stat.Contributions[j]
		identity, _ := stripDomain(c.Email)
		percentage := c.Percentage
		nlines := c.Nlines
		if nlines < config.Threshold && j < numContributors-1 {
			remainingContributors = numContributors - j
			break
		}
		remainingPercentage -= percentage
		fmt.Fprint(w, "<tr>")
		fmt.Fprintf(w, "<td><span title=\"%s\">%s</span></td>", c.Email, identity)
		fmt.Fprintf(w, "<td>%d</td>", nlines)
		fmt.Fprintf(w, "<td>%s%%</td>", formatPercent(percentage))
		fmt.Fprintln(w, "</tr>")
	}
	if remainingContributors > 0 {
		fmt.Fprint(w, "<tr>")
		fmt.Fprintf(w, "<td>%d others</td>", remainingContributors)
		fmt.Fprint(w, "<td>-</td>")
		fmt.Fprintf(w, "<td>%s%%</td>", formatPercent(remainingPercentage))
		fmt.Fprintln(w, "</tr>")
	}
	fmt.Fprintln(w, "</tbody>")
	fmt.Fprintln(w, "</table>")

}

func handleHome(config *Config, result *Result, w http.ResponseWriter) {
	stats := result.LanguageStats
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := fmt.Sprintf("Contributions to %s", config.Name)
	fmt.Fprintf(w, "<title>%s</title>\n", title)
	fmt.Fprintf(w, "<h1>%s</h1>\n", title)

	fmt.Fprintln(w, "<h2>Languages</h2>")
	languageProfile(result, w)
	fmt.Fprintln(w, "<h2>People</h2>")
	peopleProfile(result, w)

	fmt.Fprintln(w, "<h2>Contributions by language</h2>")
	for i := range stats {
		languageStat(config, &stats[i], w)
	}
}

func serve(config *Config, result *Result) error {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		handleHome(config, result, w)
	})

	fmt.Printf("Listening on %s...\n", config.Listen)

	return http.ListenAndServe(config.Listen, nil)
}

func runApp(config *Config) error {
	fmt.Printf("Analysing the repository %s...\n", config.Root)

	contents, linguistError := runLinguist(config.Root)

	if linguistError != nil {
		return fmt.Errorf("Error: %v", linguistError)
	}

	stats, statsError := getStats(config.Root, &contents)

	if statsError != nil {
		return fmt.Errorf("Error: %v", statsError)
	}

	result := &Result{
		Contents:      contents,
		LanguageStats: stats,
	}

	return serve(config, result)
}

func main() {
	app := &cli.App{
		Name:  "contributors",
		Usage: "Analyse contributors of the project",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Value:   8888,
				Usage:   "Port number",
			},
			&cli.IntFlag{
				Name:  "threshold",
				Value: 15,
				Usage: "Ignore those who contributed less than `LINES`",
			},
		},
		Action: func(c *cli.Context) error {
			config := Config{}
			if c.NArg() > 0 {
				root, rootErr := makeAbsolute(c.Args().First())
				if rootErr != nil {
					return rootErr
				}
				config.Root = root
			} else {
				root, rootErr := os.Getwd()
				if rootErr != nil {
					return fmt.Errorf("Error from Getwd: %v", rootErr)
				}
				config.Root = root
			}
			config.Listen = fmt.Sprintf(":%d", c.Int("port"))
			config.Threshold = c.Int("threshold")
			_, name := path.Split(config.Root)
			config.Name = name

			configError := verifyConfig(&config)
			if configError != nil {
				return configError
			}

			return runApp(&config)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
