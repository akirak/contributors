package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/urfave/cli/v2"
)

type RepoContents map[string][]string

func RunLinguist(root string) (RepoContents, error) {
	cmd := exec.Command("linguist", root, "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	if err != nil {
		return nil, err
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

func GetContributions(commit *object.Commit, files []string) ([]Contribution, int, error) {
	m := make(map[string]int)
	var totalLines int
	totalLines = 0
	for i := range files {
		blame, bErr := git.Blame(commit, files[i])
		if bErr != nil {
			return nil, 0, bErr
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

func GetStats(root string, contents RepoContents) ([]LanguageStat, error) {
	var result []LanguageStat

	repo, openErr := git.PlainOpen(root)
	if openErr != nil {
		return nil, openErr
	}

	ref, refErr := repo.Head()
	if refErr != nil {
		return nil, refErr
	}

	commit, commitErr := repo.CommitObject(ref.Hash())

	if commitErr != nil {
		return nil, commitErr
	}

	for language, files := range contents {
		contributions, totalLines, err := GetContributions(commit, files)
		if err != nil {
			return nil, err
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

func GenerateReport(root string, outDir string, outDirExists bool) error {
	contents, err := RunLinguist(root)

	if err != nil {
		return err
	}

	stats, pError := GetStats(root, contents)

	if pError != nil {
		return pError
	}

	for i := range stats {
		fmt.Printf("Language: %s\n", stats[i].Language)
		fmt.Printf("  Total lines: %d\n", stats[i].TotalLines)
		// fmt.Println("  Files:")
		// for j := range stats[i].Files {
		// 	fmt.Println("  -", stats[i].Files[j])
		// }
		fmt.Println("  Contributors:")
		for j := range stats[i].Contributions {
			c := stats[i].Contributions[j]
			fmt.Printf("  | %5.1f%% | %-30s | \n", c.Percentage, c.Email)
		}
		fmt.Println("---")
	}

	if !outDirExists {
		mkdirErr := os.Mkdir(outDir, 0755)
		if mkdirErr != nil {
			return mkdirErr
		}
	}

	return nil
}

func main() {
	app := &cli.App{
		Name:  "contributors",
		Usage: "Analyse contributors of the project",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "out-dir",
				Aliases:  []string{"o"},
				Usage:    "Save files to `DIR`",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Allow overwriting files to out-dir",
			},
		},
		Action: func(c *cli.Context) error {

			cwd, err := os.Getwd()
			if err != nil {
				return cli.Exit(err, 1)
			}

			var root string
			if c.NArg() > 0 {
				root = path.Join(cwd, c.Args().First())
			} else {
				root = cwd
			}
			rootInfo, rootErr := os.Stat(root)
			if rootErr != nil {
				return cli.Exit(rootErr, 1)
			} else if !rootInfo.IsDir() {
				return cli.Exit(fmt.Sprintf("%s is not a directory", root), 1)
			}

			outDir := path.Join(cwd, c.String("out-dir"))
			outDirInfo, outDirErr := os.Stat(outDir)
			if outDirErr == nil && !c.Bool("force") {
				return cli.Exit(fmt.Sprintf("%s already exists and --force is not specified", outDir), 1)
			}
			if outDirErr == nil && !outDirInfo.IsDir() {
				return cli.Exit(fmt.Sprintf("%s already exists", outDir), 1)
			}

			outDirExists := outDirErr != nil
			runErr := GenerateReport(root, outDir, outDirExists)
			if runErr != nil {
				return cli.Exit(runErr, 1)
			}

			return nil
		},
	}

	app.Run(os.Args)
}
