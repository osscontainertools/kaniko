/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dockerfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

func ParseStages(opts *config.KanikoOptions) ([]instructions.Stage, []instructions.ArgCommand, error) {
	var err error
	var d []uint8
	match, _ := regexp.MatchString("^https?://", opts.DockerfilePath)
	if match {
		response, e := http.Get(opts.DockerfilePath) //nolint:noctx
		if e != nil {
			return nil, nil, e
		}
		d, err = io.ReadAll(response.Body)
	} else {
		d, err = os.ReadFile(opts.DockerfilePath)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("reading dockerfile at path %s: %w", opts.DockerfilePath, err)
	}

	stages, metaArgs, err := Parse(d)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing dockerfile: %w", err)
	}

	metaArgs, err = expandNestedArgs(metaArgs, opts.BuildArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("expanding meta ARGs: %w", err)
	}

	return stages, metaArgs, nil
}

// baseImageIndex returns the index of the stage the current stage is built off
// returns -1 if the current stage isn't built off a previous stage
func baseImageIndex(currentStage int, stages []instructions.Stage) int {
	currentStageBaseName := strings.ToLower(stages[currentStage].BaseName)

	for i, stage := range stages {
		if i >= currentStage {
			break
		}
		if stage.Name == currentStageBaseName {
			return i
		}
	}

	return -1
}

// Parse parses the contents of a Dockerfile and returns a list of commands
func Parse(b []byte) ([]instructions.Stage, []instructions.ArgCommand, error) {
	p, err := parser.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	stages, metaArgs, err := instructions.Parse(p.AST, &linter.Linter{})
	if err != nil {
		return nil, nil, err
	}

	metaArgs, err = stripEnclosingQuotes(metaArgs)
	if err != nil {
		return nil, nil, err
	}

	return stages, metaArgs, nil
}

// expandNestedArgs tries to resolve nested ARG value against the previously defined ARGs
func expandNestedArgs(metaArgs []instructions.ArgCommand, buildArgs []string) ([]instructions.ArgCommand, error) {
	var prevArgs []string
	for i, marg := range metaArgs {
		for j, arg := range marg.Args {
			v := arg.Value
			if v != nil {
				val, err := util.ResolveEnvironmentReplacement(*v, append(prevArgs, buildArgs...), false)
				if err != nil {
					return nil, err
				}
				prevArgs = append(prevArgs, arg.Key+"="+val)
				arg.Value = &val
				metaArgs[i].Args[j] = arg
			}
		}
	}
	return metaArgs, nil
}

// stripEnclosingQuotes removes quotes enclosing the value of each instructions.ArgCommand in a slice
// if the quotes are escaped it leaves them
func stripEnclosingQuotes(metaArgs []instructions.ArgCommand) ([]instructions.ArgCommand, error) {
	for i, marg := range metaArgs {
		for j, arg := range marg.Args {
			v := arg.Value
			if v != nil {
				val, err := extractValFromQuotes(*v)
				if err != nil {
					return nil, err
				}

				arg.Value = &val
				metaArgs[i].Args[j] = arg
			}
		}
	}
	return metaArgs, nil
}

func extractValFromQuotes(val string) (string, error) {
	backSlash := byte('\\')
	if len(val) < 2 {
		return val, nil
	}

	var leader string
	var tail string

	switch char := val[0]; char {
	case '\'', '"':
		leader = string([]byte{char})
	case backSlash:
		switch char := val[1]; char {
		case '\'', '"':
			leader = string([]byte{backSlash, char})
		}
	}

	// If the length of leader is greater than one then it must be an escaped
	// character.
	if len(leader) < 2 {
		switch char := val[len(val)-1]; char {
		case '\'', '"':
			tail = string([]byte{char})
		}
	} else {
		switch char := val[len(val)-2:]; char {
		case `\'`, `\"`:
			tail = char
		}
	}

	if leader != tail {
		logrus.Infof("Leader %s tail %s", leader, tail)
		return "", errors.New("quotes wrapping arg values must be matched")
	}

	if leader == "" {
		return val, nil
	}

	if len(leader) == 2 {
		return val, nil
	}

	return val[1 : len(val)-1], nil
}

// targetStage returns the index of the target stage kaniko is trying to build
func targetStage(stages []instructions.Stage, target string) (int, error) {
	if target == "" {
		return len(stages) - 1, nil
	}
	for i, stage := range stages {
		if strings.EqualFold(stage.Name, target) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%s is not a valid target build stage", target)
}

// ParseCommands parses an array of commands into an array of instructions.Command; used for onbuild
func ParseCommands(cmdArray []string) ([]instructions.Command, error) {
	if len(cmdArray) == 0 {
		return []instructions.Command{}, nil
	}
	var cmds []instructions.Command
	cmdString := strings.Join(cmdArray, "\n")
	ast, err := parser.Parse(strings.NewReader(cmdString))
	if err != nil {
		return nil, err
	}
	for _, child := range ast.AST.Children {
		cmd, err := instructions.ParseCommand(child)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

// SaveStage returns true if the current stage will be needed later in the Dockerfile
func saveStage(index int, stages []instructions.Stage) bool {
	currentStageName := stages[index].Name

	for stageIndex, stage := range stages {
		if stageIndex <= index {
			continue
		}

		if strings.ToLower(stage.BaseName) == currentStageName {
			if stage.BaseName != "" {
				return true
			}
		}
	}

	return false
}

// ResolveCrossStageCommands resolves any calls to previous stages with names to indices
// Ex. --from=secondStage should be --from=1 for easier processing later on
// As third party library lowers stage name in FROM instruction, this function resolves stage case insensitively.
func ResolveCrossStageCommands(cmds []instructions.Command, stageNameToIdx map[string]int) {
	for _, cmd := range cmds {
		switch c := cmd.(type) {
		case *instructions.CopyCommand:
			if c.From != "" {
				if val, ok := stageNameToIdx[strings.ToLower(c.From)]; ok {
					c.From = strconv.Itoa(val)
				}
			}
		}
	}
}

// resolveStagesArgs resolves all the args from list of stages
func resolveStagesArgs(stages []instructions.Stage, args []string) error {
	for i, s := range stages {
		resolvedBaseName, err := util.ResolveEnvironmentReplacement(s.BaseName, args, false)
		if err != nil {
			return fmt.Errorf("resolving base name %s: %w", s.BaseName, err)
		}
		if s.BaseName != resolvedBaseName {
			stages[i].BaseName = resolvedBaseName
		}
	}
	return nil
}

func MakeKanikoStages(opts *config.KanikoOptions, stages []instructions.Stage, metaArgs []instructions.ArgCommand) ([]config.KanikoStage, error) {
	targetStage, err := targetStage(stages, opts.Target)
	if err != nil {
		return nil, fmt.Errorf("error finding target stage: %w", err)
	}
	args := unifyArgs(metaArgs, opts.BuildArgs)
	if err := resolveStagesArgs(stages, args); err != nil {
		return nil, fmt.Errorf("resolving args: %w", err)
	}
	stages = stages[:targetStage+1]

	stageByName := make(map[string]int)
	for idx, s := range stages {
		if s.Name != "" {
			stageByName[s.Name] = idx
		}
	}

	kanikoStages := make([]config.KanikoStage, len(stages))
	// We now "count" references, it is only safe to squash
	// stages if the references are exactly 1 and there are no COPY references
	stagesDependencies := make([]int, len(stages))
	copyDependencies := make([]int, len(stages))
	stagesDependencies[targetStage] = 1
	for i := targetStage; i >= 0; i-- {
		if stagesDependencies[i] == 0 && copyDependencies[i] == 0 && opts.SkipUnusedStages {
			continue
		}
		stage := stages[i]
		if len(stage.Name) > 0 {
			logrus.Infof("Resolved base name of %s to %s", stage.Name, stage.BaseName)
		}
		baseImageIndex := baseImageIndex(i, stages)

		var onBuild []string
		if stage.BaseName == constants.NoBaseImage {
			// pass
		} else if baseImageIndex != -1 {
			onBuild = getOnBuild(stages[baseImageIndex].Commands)
		} else {
			image, err := remote.RetrieveRemoteImage(stage.BaseName, opts.RegistryOptions, opts.CustomPlatform)
			if err != nil {
				return nil, err
			}
			cfg, err := image.ConfigFile()
			if err != nil {
				return nil, err
			}
			onBuild = cfg.Config.OnBuild
		}
		cmds, err := ParseCommands(onBuild)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ONBUILD instructions: %w", err)
		}
		stage.Commands = append(cmds, stage.Commands...)

		if opts.SkipUnusedStages {
			if baseImageIndex != -1 {
				stagesDependencies[baseImageIndex]++
			}
			for _, c := range stage.Commands {
				switch cmd := c.(type) {
				case *instructions.CopyCommand:
					if copyFromIndex, err := strconv.Atoi(cmd.From); err == nil {
						// numeric reference `COPY --from=0`
						copyDependencies[copyFromIndex]++
					} else {
						// named reference `COPY --from=base`
						if copyFromIndex, ok := stageByName[strings.ToLower(cmd.From)]; ok {
							// There can be references that appear as non-existing stages
							// ie. `COPY --from=debian` would try refer to `debian` as stage
							// before falling back to `debian` as a docker image.
							copyDependencies[copyFromIndex]++
						}
					}
				}
			}
		}
		kanikoStages[i] = config.KanikoStage{
			Stage:                  stage,
			BaseImageIndex:         baseImageIndex,
			BaseImageStoredLocally: (baseImageIndex != -1),
			SaveStage:              saveStage(i, stages),
			Final:                  i == targetStage,
			MetaArgs:               metaArgs,
			Index:                  i,
		}
	}
	if opts.SkipUnusedStages && config.EnvBoolDefault("FF_KANIKO_SQUASH_STAGES", true) {
		for i, s := range kanikoStages {
			if stagesDependencies[i] > 0 {
				if s.BaseImageStoredLocally && stagesDependencies[s.BaseImageIndex] == 1 && copyDependencies[s.BaseImageIndex] == 0 {
					sb := kanikoStages[s.BaseImageIndex]
					// squash stages[i] into stages[i].BaseName
					logrus.Infof("Squashing stages: %s into %s", s.Name, sb.Name)
					// We squash the base stage into the current stage because,
					// no one else depends on the base stage so it can be freely moved,
					// the current stage might depend on other stages so it is not safe to move it.
					kanikoStages[i] = squash(sb, s)
					stagesDependencies[s.BaseImageIndex] = 0
				}
			}
		}
	}
	if opts.SkipUnusedStages {
		var onlyUsedStages []config.KanikoStage
		for i, s := range kanikoStages {
			if stagesDependencies[i] > 0 || copyDependencies[i] > 0 {
				s.SaveStage = stagesDependencies[i] > 0
				onlyUsedStages = append(onlyUsedStages, s)
			}
		}
		kanikoStages = onlyUsedStages
	}
	return kanikoStages, nil
}

// unifyArgs returns the unified args between metaArgs and --build-arg
// by default --build-arg overrides metaArgs except when --build-arg is empty
func unifyArgs(metaArgs []instructions.ArgCommand, buildArgs []string) []string {
	argsMap := make(map[string]string)
	for _, marg := range metaArgs {
		for _, arg := range marg.Args {
			if arg.Value != nil {
				argsMap[arg.Key] = *arg.Value
			}
		}
	}
	splitter := "="
	for _, a := range buildArgs {
		s := strings.Split(a, splitter)
		if len(s) > 1 && s[1] != "" {
			argsMap[s[0]] = s[1]
		}
	}
	var args []string
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	return args
}

func getOnBuild(cmds []instructions.Command) []string {
	var out []string
	for _, c := range cmds {
		switch cmd := c.(type) {
		case *instructions.OnbuildCommand:
			out = append(out, cmd.Expression)
		}
	}
	return out
}

func filterOnBuild(cmds []instructions.Command) []instructions.Command {
	var out []instructions.Command
	for _, c := range cmds {
		switch cmd := c.(type) {
		case *instructions.OnbuildCommand:
			// Skip ONBUILD commands
		default:
			out = append(out, cmd)
		}
	}
	return out
}

func squash(a, b config.KanikoStage) config.KanikoStage {
	acmds := filterOnBuild(a.Commands)
	return config.KanikoStage{
		Stage: instructions.Stage{
			Name:       b.Name,
			Commands:   append(acmds, b.Commands...),
			OrigCmd:    a.OrigCmd,
			BaseName:   a.BaseName,
			Platform:   a.Platform,
			DocComment: a.DocComment + b.DocComment,
			SourceCode: a.SourceCode + "\n" + b.SourceCode,
			Location:   append(a.Location, b.Location...),
			Comments:   append(a.Comments, b.Comments...),
		},
		BaseImageIndex:         a.BaseImageIndex,
		Final:                  b.Final,
		BaseImageStoredLocally: a.BaseImageStoredLocally,
		SaveStage:              b.SaveStage,
		MetaArgs:               append(a.MetaArgs, b.MetaArgs...),
		Index:                  b.Index,
	}
}
