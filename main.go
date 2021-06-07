package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/urfave/cli/v2"
)

type Config struct {
	Root   string
	Listen string
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

func getContributions(commit *object.Commit, files []string) ([]Contribution, int, error) {
	m := make(map[string]int)
	var totalLines int
	totalLines = 0
	for i := range files {
		file := files[i]
		blame, bErr := git.Blame(commit, file)
		if bErr != nil {
			return nil, 0, fmt.Errorf("git-blame failed on %s: %v", file, bErr)
		}
		for lineNum := range blame.Lines {
			author := blame.Lines[lineNum].Author
			n, ok := m[author]
			if !ok {
				m[author] = 1
			} else {
				m[author] = n + 1
			}
			totalLines++
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

func getStats(root string, contents RepoContents) ([]LanguageStat, error) {
	var result []LanguageStat

	repo, openErr := git.PlainOpen(root)
	if openErr != nil {
		return nil, fmt.Errorf("Failed to open the repository %s: %v", root, openErr)
	}

	ref, refErr := repo.Head()
	if refErr != nil {
		return nil, fmt.Errorf("Failed to read the head reference: %v", refErr)
	}

	commit, commitErr := repo.CommitObject(ref.Hash())

	if commitErr != nil {
		return nil, fmt.Errorf("Failed to retrieve the commit: %v", commitErr)
	}

	for language, files := range contents {
		contributions, totalLines, err := getContributions(commit, files)
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

func runApp(config Config) error {
	fmt.Printf("Analysing the repository %s...\n", config.Root)

	contents, linguistError := runLinguist(config.Root)

	if linguistError != nil {
		return fmt.Errorf("Error: %v", linguistError)
	}

	stats, statsError := getStats(config.Root, contents)

	if statsError != nil {
		return fmt.Errorf("Error: %v", statsError)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<title>Contributors on %s</title>\n", config.Root)
		fmt.Fprintf(w, "<h1>Contributors on %s</h1>\n", config.Root)

		fmt.Fprintln(w, "<h2>Profile</h2>")

		fmt.Fprintln(w, "<table>")
		fmt.Fprintln(w, "<thead><tr>")
		fmt.Fprint(w, "<th>Language</th>")
		fmt.Fprint(w, "<th># of lines</th>")
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
			fmt.Fprintf(w, "<td>%d</td>", stat.TotalLines)
			percentage := float64(stat.TotalLines) / float64(totalLines) * 100
			fmt.Fprintf(w, "<td>%.2f%%</td>", percentage)
			fmt.Fprintln(w, "</tr>")
		}
		fmt.Fprintln(w, "</tbody>")
		fmt.Fprintln(w, "</table>")

		fmt.Fprintln(w, "<h2>Contributions by language</h2>")
		for i := range stats {
			stat := stats[i]
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
			fmt.Fprintln(w, "<th>Percent</th>")
			fmt.Fprintln(w, "</tr>")
			fmt.Fprintln(w, "</thead>")
			fmt.Fprintln(w, "<tbody>")
			for j := range stat.Contributions {
				c := stat.Contributions[j]
				fmt.Fprint(w, "<tr>")
				fmt.Fprintf(w, "<td>%s</td>", c.Email)
				fmt.Fprintf(w, "<td>%.1f%%</td>", c.Percentage)
				fmt.Fprintln(w, "</tr>")
			}
			fmt.Fprintln(w, "</tbody>")
			fmt.Fprintln(w, "</table>")

		}
	})

	fmt.Printf("Listening on %s...\n", config.Listen)

	return http.ListenAndServe(config.Listen, nil)
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

			configError := verifyConfig(&config)
			if configError != nil {
				return configError
			}

			return runApp(config)
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
