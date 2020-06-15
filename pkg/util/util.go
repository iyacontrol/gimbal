/*
Copyright 2018 the Gimbal contributors.
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

// Package util contains reference utilities
package util

import (
	"regexp"

	"github.com/sirupsen/logrus"
)

var backendNameRegex = regexp.MustCompile("^[a-z]([-a-z0-9]*[a-z0-9])?$")

// GetFormatter returns a textformatter to customize logs
func GetFormatter() *logrus.TextFormatter {
	return &logrus.TextFormatter{
		FullTimestamp: true,
	}
}

// IsInvalidBackendName returns true if valid cluster name
func IsInvalidBackendName(backendname string) bool {
	return !backendNameRegex.MatchString(backendname)
}

// Find takes a slice and looks for an element in it. If found it will
// return true, otherwise it will return a bool of false.
func Find(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
