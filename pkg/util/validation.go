/*
Copyright 2014 Google Inc. All rights reserved.

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

package util

import (
	"regexp"
)

var dnsLabelFmt string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
var dnsLabelRegexp *regexp.Regexp = regexp.MustCompile("^" + dnsLabelFmt + "$")

// IsDNSLabel tests for a string that conforms to the definition of a label in
// DNS (RFC 1035/1123).  This checks the format, but not the length.
func IsDNSLabel(value string) bool {
	return dnsLabelRegexp.MatchString(value)
}

var dnsSubdomainFmt string = dnsLabelFmt + "(\\." + dnsLabelFmt + ")*"
var dnsSubdomainRegexp *regexp.Regexp = regexp.MustCompile("^" + dnsSubdomainFmt + "$")

// IsDNSSubdomain tests for a string that conforms to the definition of a
// subdomain in DNS (RFC 1035/1123).  This checks the format, but not the length.
func IsDNSSubdomain(value string) bool {
	return dnsSubdomainRegexp.MatchString(value)
}

var cIdentifierFmt string = "[A-Za-z_][A-Za-z0-9_]*"
var cIdentifierRegexp *regexp.Regexp = regexp.MustCompile("^" + cIdentifierFmt + "$")

// IsCIdentifier tests for a string that conforms the definition of an identifier
// in C. This checks the format, but not the length.
func IsCIdentifier(value string) bool {
	return cIdentifierRegexp.MatchString(value)
}

// IsValidPortNum tests that the argument is a valid, non-zero port number.
func IsValidPortNum(port int) bool {
	return 0 < port && port < 65536
}
