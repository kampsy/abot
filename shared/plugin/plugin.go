// Package plugin enables plugins to register with Abot and connect to the
// database.
package plugin

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dchest/stemmer/porter2"
	"github.com/itsabot/abot/core"
	"github.com/itsabot/abot/core/log"
	"github.com/itsabot/abot/shared/datatypes"
	"github.com/itsabot/abot/shared/language"
	_ "github.com/lib/pq" // Import the pq PostgreSQL driver
)

// ErrMissingPluginName is returned when a plugin name is expected, but
// but a blank name is provided.
var ErrMissingPluginName = errors.New("missing plugin name")

// ErrMissingTrigger is returned when a trigger is expected but none
// were found.
var ErrMissingTrigger = errors.New("missing plugin trigger")

// New builds a Plugin with its trigger, RPC, and configuration settings from
// its plugin.json.
func New(url string) (*dt.Plugin, error) {
	if err := core.LoadEnvVars(); err != nil {
		log.Fatal(err)
	}
	db, err := core.ConnectDB("")
	if err != nil {
		return nil, err
	}

	// Read plugin.json data from within plugins.go, unmarshal into struct
	c := dt.PluginConfig{}
	if len(os.Getenv("ABOT_PATH")) > 0 {
		p := filepath.Join(os.Getenv("ABOT_PATH"), "plugins.go")
		var scn *bufio.Scanner
		fi, err := os.OpenFile(p, os.O_RDONLY, 0666)
		if os.IsNotExist(err) {
			goto makePlugin
		}
		if err != nil {
			return nil, err
		}
		defer func() {
			if err = fi.Close(); err != nil {
				log.Info("failed to close file", fi.Name())
				return
			}
		}()
		var found bool
		var data string
		scn = bufio.NewScanner(fi)
		for scn.Scan() {
			t := scn.Text()
			if !found && t != url {
				continue
			} else if t == url {
				found = true
				continue
			} else if len(t) >= 1 && t[0] == '}' {
				data += t
				break
			}
			data += t
		}
		if err = scn.Err(); err != nil {
			return nil, err
		}
		if len(data) > 0 {
			if err = json.Unmarshal([]byte(data), &c); err != nil {
				return nil, err
			}
			if len(c.Name) == 0 {
				return nil, ErrMissingPluginName
			}
		}
	}
makePlugin:
	l := log.New(c.Name)
	l.SetDebug(os.Getenv("ABOT_DEBUG") == "true")
	plg := &dt.Plugin{
		Trigger:     &dt.StructuredInput{},
		SetBranches: func(in *dt.Msg) [][]dt.State { return nil },
		Events: &dt.PluginEvents{
			PostReceive:    func(cmd *string) {},
			PreProcessing:  func(cmd *string, u *dt.User) {},
			PostProcessing: func(in *dt.Msg) {},
			PreResponse:    func(in *dt.Msg, resp *string) {},
		},
		Config: c,
		DB:     db,
		Log:    l,
	}
	plg.SM = dt.NewStateMachine(plg)
	return plg, nil
}

// Register enables Abot to notify plugins when specific StructuredInput is
// encountered matching triggers set in the plugins themselves. Note that
// plugins will only listen when (Command and Object) or (Intent) criteria are
// met. There's no support currently for duplicate routes, e.g.
// "find_restaurant" leading to either one of two plugins.
func Register(p *dt.Plugin) error {
	p.Log.Debug("registering", p.Config.Name)
	for _, i := range p.Trigger.Intents {
		s := "I_" + strings.ToLower(i)
		oldPlg := core.RegPlugins.Get(s)
		if oldPlg != nil && oldPlg.Config.Name != p.Config.Name {
			p.Log.Infof("found duplicate plugin or trigger %s on %s",
				p.Config.Name, s)
		}
		core.RegPlugins.Set(s, p)
	}
	eng := porter2.Stemmer
	for _, c := range p.Trigger.Commands {
		c = strings.ToLower(eng.Stem(c))
		for _, o := range p.Trigger.Objects {
			o = strings.ToLower(eng.Stem(o))
			s := "CO_" + c + "_" + o
			oldPlg := core.RegPlugins.Get(s)
			if oldPlg != nil && oldPlg.Config.Name != p.Config.Name {
				p.Log.Info("found duplicate plugin or trigger",
					p.Config.Name, "on", s)
			}
			core.RegPlugins.Set(s, p)
		}
	}

	// registerPlugin is called whenever Keywords or Triggers are changed,
	// but we don't want to append duplicate entries to our
	// core.AllPlugins.
	for _, plg := range core.AllPlugins {
		if plg.Config.Name == p.Config.Name {
			return nil
		}
	}
	core.AllPlugins = append(core.AllPlugins, p)
	p.SM.SetStates([][]dt.State{p.States})
	return nil
}

// SetKeywords processes and registers keywords with Abot's core for routing.
func SetKeywords(p *dt.Plugin, khs ...dt.KeywordHandler) {
	p.Keywords = &dt.Keywords{
		Dict: map[string]dt.KeywordFn{},
	}
	for _, kh := range khs {
		for _, intent := range kh.Trigger.Intents {
			intent = strings.ToLower(intent)
			if !language.Contains(p.Trigger.Intents, intent) {
				p.Trigger.Intents = append(p.Trigger.Intents, intent)
			}
			key := "I_" + intent
			_, exists := p.Keywords.Dict[key]
			if exists {
				continue
			}
			p.Keywords.Dict[key] = kh.Fn
		}
		eng := porter2.Stemmer
		for _, cmd := range kh.Trigger.Commands {
			cmd = strings.ToLower(eng.Stem(cmd))
			if !language.Contains(p.Trigger.Commands, cmd) {
				p.Trigger.Commands = append(p.Trigger.Commands, cmd)
			}
			for _, obj := range kh.Trigger.Objects {
				obj = strings.ToLower(eng.Stem(obj))
				if !language.Contains(p.Trigger.Objects, obj) {
					p.Trigger.Objects = append(p.Trigger.Objects, obj)
				}
				key := "CO_" + cmd + "_" + obj
				p.Keywords.Dict[key] = kh.Fn
			}
		}
	}
}

// SetStates is a convenience function provided to match the API of NewKeywords
// and AppendTrigger.
func SetStates(p *dt.Plugin, states [][]dt.State) {
	p.States = []dt.State{}
	for _, ss := range states {
		p.States = append(p.States, ss...)
	}
}

// AppendTrigger appends the StructuredInput's modified contents to a plugin.
// All Commands and Objects stemmed using the Porter2 Snowball algorithm.
func AppendTrigger(p *dt.Plugin, t *dt.StructuredInput) {
	eng := porter2.Stemmer
	for _, cmd := range t.Commands {
		cmd = eng.Stem(cmd)
		if !language.Contains(p.Trigger.Commands, cmd) {
			p.Trigger.Commands = append(p.Trigger.Commands, cmd)
		}
	}
	for _, obj := range t.Objects {
		obj = eng.Stem(obj)
		if !language.Contains(p.Trigger.Objects, obj) {
			p.Trigger.Objects = append(p.Trigger.Objects, obj)
		}
	}
}
