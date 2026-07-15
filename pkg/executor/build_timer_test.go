/*
Copyright 2026 OSS Container Tools

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

package executor

import (
	"errors"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/osscontainertools/kaniko/pkg/commands"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util"
)

type failingExecCommand struct{ MockDockerCommand }

func (f failingExecCommand) ExecuteCommand(_ *v1.Config, _ *dockerfile.BuildArgs) error {
	return errors.New("exec failed")
}

// Pins the span-leak fix: a command failing mid-build must still stop its
// timer — an unended timer is an unended span, which is never exported, and
// the failing command's span is the one most worth having in a trace.
func TestBuildStopsCommandTimerOnError(t *testing.T) {
	const marker = "RUN timer-leak-regression-pin"
	sb := &stageBuilder{
		args:  dockerfile.NewBuildArgs([]string{}),
		image: fakeImage{},
		cf:    &v1.ConfigFile{Config: v1.Config{}},
		cmds:  []commands.DockerCommand{failingExecCommand{MockDockerCommand{command: marker}}},
	}
	err := sb.build(*NewCompositeCache(""), &config.KanikoOptions{}, util.FileContext{}, &fakeSnapShotter{}, false, nil, nil)
	if err == nil {
		t.Fatal("expected build error from failing command")
	}
	js, jerr := timing.JSON()
	if jerr != nil {
		t.Fatalf("timing.JSON: %v", jerr)
	}
	if !strings.Contains(js, "Command: "+marker) {
		t.Errorf("timer for failing command was not stopped; categories: %s", js)
	}
}
