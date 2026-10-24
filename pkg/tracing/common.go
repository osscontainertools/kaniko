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

// Package tracing exports kaniko build traces over OTLP, opt-in via
// KANIKO_TELEMETRY_ENDPOINT.
package tracing

import "strings"

// enables tracing when set to an OTLP-HTTP collector URL.
const EndpointEnv = "KANIKO_TELEMETRY_ENDPOINT"

const BuildIDEnv = "KANIKO_TELEMETRY_BUILD_ID"

// whether to keep the Dockerfile source out of the trace.
const OmitDockerfileEnv = "KANIKO_TELEMETRY_OMIT_DOCKERFILE"

func RedactCommand(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return cmd
	}
	k := 0
	if len(fields) > 1 && strings.EqualFold(fields[0], "ONBUILD") {
		k = 1
	}
	switch strings.ToUpper(fields[k]) {
	case "RUN", "ENV", "ARG":
		n := k + 1
		for n < len(fields) && strings.HasPrefix(fields[n], "--") {
			n++
		}
		return strings.Join(fields[:n], " ")
	default:
		return cmd
	}
}
