package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/celicoo/docli"
	"github.com/darvid/stashvision/stashvision-go"
	"github.com/imroc/req"
	"github.com/jmespath/go-jmespath"
	"github.com/shiena/ansicolor"
	log "github.com/sirupsen/logrus"
)

func enableVerboseLogging() {
	log.SetLevel(log.DebugLevel)
	req.Debug = true
}

type Query struct {
	Filter, QueryString      string
	Json, Positions, Verbose bool
}

func (q *Query) Doc() string {
	return `usage: stashvision query [<arguments>] -s=<query-string>

arguments:
  -f, --filter=<path>  when used with -j, filter results with jmespath
  -j, --json           return items as JSON
  -p, --positions      return item positions and dimensions only
  -s, --query-string   querystring
  -v, --verbose        show debug log messages
`
}

func (q *Query) Error(err error) {
	log.Fatalln(err)
}

func (q *Query) Help() {
	fmt.Println(q.Doc())
}

func (q *Query) Run() {
	if q.QueryString == "" {
		log.Error("query string is required")
		q.Help()
		return
	}
	if q.Verbose {
		enableVerboseLogging()
	}
	index, err := stashvision.CreateOrOpenIndex(&map[string]interface{}{
		"read_only": true,
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer index.Close()
	items, err := stashvision.QueryIndex(q.QueryString, index, stashvision.PoeQuadTabSize)
	if err != nil {
		log.Fatalln(err)
	}
	if q.Positions {
		for _, item := range items {
			fmt.Println(item.PositionString())
		}
		return
	} else if !q.Json {
		log.Infof("%d search results", len(items))
		for i, item := range items {
			log.Infof("- %d: %s", i+1, item.ToString())
		}
	} else {
		var b []byte
		if q.Filter != "" {
			var filtered []interface{}
			for _, item := range items {
				filterResult, err := jmespath.Search(q.Filter, interface{}(item))
				if err != nil {
					log.Fatalln(err)
				}
				filtered = append(filtered, filterResult)
			}
			b, _ = json.MarshalIndent(filtered, "", "  ")
		} else {
			b, _ = json.MarshalIndent(items, "", "  ")
		}
		fmt.Println(string(b))
	}
}

type Recipe struct {
	TabIndex                               int
	RecipeName                             string
	First, ListRecipes, Positions, Verbose bool
}

func (r *Recipe) Doc() string {
	return `usage: stashvision recipe [<arguments>] -n=<recipe-name>

arguments:
  -f, --first          return the first set of items found
  -l, --list-recipes   list available recipes
  -n, --recipe-name    recipe name
  -p, --positions      return item positions and dimensions only
  -t, --tab-index      tab index (default 0)
  -v, --verbose        show debug log messages
`
}

func (r *Recipe) Error(err error) {
	log.Fatalln(err)
}

func (r *Recipe) Help() {
	fmt.Println(r.Doc())
}

func (r *Recipe) Run() {
	if r.ListRecipes {
		for recipeName, _ := range stashvision.RecipeFactories {
			log.Println(recipeName)
		}
		return
	}
	if r.Verbose {
		enableVerboseLogging()
	}
	if r.RecipeName == "" {
		log.Error("recipe name required")
		r.Help()
		return
	}
	recipeFactory, ok := stashvision.RecipeFactories[r.RecipeName]
	if !ok {
		log.Error("invalid recipe name, use -l for list")
		return
	}
	index, err := stashvision.CreateOrOpenIndex(&map[string]interface{}{
		"read_only": true,
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer index.Close()
	recipe, err := recipeFactory()
	if err != nil {
		log.Fatalln(err)
	}
	results, err := recipe.ScanIndex(nil, r.TabIndex, index, !r.First)
	if err != nil {
		log.Fatalln(err)
	}
	if r.Positions {
		for _, result := range results {
			for _, item := range result.Items {
				fmt.Println(item.PositionString())
			}
			fmt.Println("---")
		}
	} else {
		totalCurrency := make(map[string]int)
		totalItems := 0
		for _, result := range results {
			reward := result.Reward
			if reward.Item != nil {
				totalItems++
			}
			if reward.Currency != nil {
				if value, ok := totalCurrency[reward.Currency.Name]; ok {
					totalCurrency[reward.Currency.Name] = value + reward.Currency.Value
				} else {
					totalCurrency[reward.Currency.Name] = reward.Currency.Value
				}
			}
			for _, item := range result.Items {
				log.Printf("%s", item.Class)
			}
		}
		log.WithFields(log.Fields{
			"numResults":    len(results),
			"totalCurrency": totalCurrency,
			"totalItems":    totalItems,
		}).Info("completed recipe scan")
	}
}

type Server struct {
	AccountName, LeagueName, LogFile, PoeSessionId string
	TabIndex                                       int
	Verbose                                        bool
}

func (s *Server) Doc() string {
	return `usage: stashvision server [<arguments>] -a=<account name> -s=<poe session id>

arguments:
  -a, --account-name    account name
  -L, --league-name     league name
  -l, --log-file=<file> log to a local file instead of stderr
  -s, --poe-session-id  value of POESESSID
  -t, --tab-index       tab index (default 0)
  -v, --verbose         show debug log messages
`
}

func (s *Server) Error(err error) {
	log.Fatalln(err)
}

func (s *Server) Help() {
	fmt.Println(s.Doc())
}

func (s *Server) Run() {
	if s.LogFile != "" {
		log.SetFormatter(&log.TextFormatter{ForceColors: false})
		f, err := os.OpenFile(s.LogFile, os.O_WRONLY|os.O_CREATE, 0755)
		if err != nil {
			log.Fatalln(err)
		}
		log.SetOutput(f)
	}
	if s.PoeSessionId == "" || s.AccountName == "" {
		log.Error("missing POESESSID and/or account name")
		s.Help()
		return
	}
	if s.Verbose {
		enableVerboseLogging()
	}
	leagueName := strings.ToLower(s.LeagueName)
	stashvision.RunServer(s.PoeSessionId, s.AccountName, leagueName, s.TabIndex, nil)
}

type Stashvision struct {
	Query   Query
	Recipe  Recipe
	Server  Server
	Version bool
}

func (s *Stashvision) Doc() string {
	return `stashvision - Index and analyze your Path of Exile stash.

commands:
  q, query     query stash items index
  r, recipe    evaluate recipes against index
  s, server    run stash indexing server

usage:
  stashvision query [--json] -s=<querystring>
  stashvision server -s=<poesessionid> -a=<account_name>

arguments:
  --help             show this screen
  --version          show version
`
}

func (s *Stashvision) Error(err error) {
	log.Fatalln(err)
}

func (s *Stashvision) Help() {
	fmt.Println(s.Doc())
}

func (s *Stashvision) Run() {
	if s.Version {
		fmt.Println(stashvision.Version)
		return
	}
	s.Help()
}

func main() {
	log.SetFormatter(&log.TextFormatter{ForceColors: true})
	log.SetOutput(ansicolor.NewAnsiColorWriter(os.Stdout))

	var s Stashvision
	args := docli.Args()
	args.Bind(&s)
}
