package commands

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

var v1SupportedIncludeObjects = []string{"tables", "views"}
var v2SupportedIncludeObjects = []string{"tables", "views", "routines", "triggers", "events"}

// unsupportedV1IncludeObjects returns object types that are not part of v1.
func unsupportedV1IncludeObjects(objects []string) []string {
	unsupported := make([]string, 0, len(objects))
	for _, object := range objects {
		name := strings.TrimSpace(strings.ToLower(object))
		if name == "" {
			continue
		}
		if slices.Contains(v1SupportedIncludeObjects, name) {
			continue
		}
		unsupported = append(unsupported, name)
	}
	sort.Strings(unsupported)
	return unsupported
}

// unsupportedV2IncludeObjects returns object types that are not part of v1 or v2.
func unsupportedV2IncludeObjects(objects []string) []string {
	unsupported := make([]string, 0, len(objects))
	for _, object := range objects {
		name := strings.TrimSpace(strings.ToLower(object))
		if name == "" {
			continue
		}
		if slices.Contains(v2SupportedIncludeObjects, name) {
			continue
		}
		unsupported = append(unsupported, name)
	}
	sort.Strings(unsupported)
	return unsupported
}

func reservedV2ObjectsError(objects []string) error {
	unsupported := unsupportedV1IncludeObjects(objects)
	return fmt.Errorf(
		"--include-objects contains unsupported v1 types (%s); reserved for v2: routines, triggers, events",
		strings.Join(unsupported, ","),
	)
}
