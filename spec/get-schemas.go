package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var relevantPaths = [][]any{
	{"/v0/org/{org_id}/keys/{key_id}", []string{"get"}},
	{"/v1/org/{org_id}/blob/sign/{key_id}", make([]string, 0)},
	{"/v1/org/{org_id}/token/refresh", make([]string, 0)},
	{"/v0/org/{org_id}/roles/{role_id}/tokens", []string{"post"}},
}

func getComponentKey(ref string) (string, string) {
	path := strings.Split(ref, "/")
	componentName := path[len(path)-1]
	componentType := path[len(path)-2]
	return componentType, componentName
}

func getComponent(components map[string]any, componentType string, componentName string) any {
	return components[componentType].(map[string]any)[componentName]
}

func assignComponent(components map[string]any, newComponents map[string]map[string]any, componentType, componentName string) {
	newComponents[componentType][componentName] = getComponent(components, componentType, componentName)
}

func getRefs(obj any, refs *[]string) {
	switch v := obj.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			*refs = append(*refs, ref)
		}
		for _, item := range v {
			getRefs(item, refs)
		}
	case []any:
		for _, item := range v {
			getRefs(item, refs)
		}
	}
}

func main() {
	// Get the OpenAPI JSON from GitHub
	resp, err := http.Get("https://raw.githubusercontent.com/cubist-labs/CubeSigner-TypeScript-SDK/main/packages/sdk/spec/openapi.json")
	if err != nil {
		fmt.Println("Error fetching spec:", err)
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	var api map[string]any
	if err := json.Unmarshal(data, &api); err != nil {
		fmt.Println("Error unmarshaling JSON:", err)
		return
	}

	components := api["components"].(map[string]any)
	newComponents := make(map[string]map[string]any)

	for key, value := range components {
		if key == "schemas" || key == "responses" {
			newComponents[key] = make(map[string]any)
		} else {
			newComponents[key] = value.(map[string]any)
		}
	}

	var refs []string

	// Get all relevant refs from paths
	for _, pathMethods := range relevantPaths {
		path := pathMethods[0].(string)
		methods := pathMethods[1].([]string)

		if len(methods) == 0 {
			getRefs(api["paths"].(map[string]any)[path], &refs)
		} else {
			for _, method := range methods {
				getRefs(api["paths"].(map[string]any)[path].(map[string]any)[method], &refs)
			}
		}
	}

	// Repeat until no new refs are found
	for len(refs) != 0 {
		for _, ref := range refs {
			componentType, componentName := getComponentKey(ref)
			assignComponent(components, newComponents, componentType, componentName)
		}

		refs = []string{}
		getRefs(newComponents["schemas"], &refs)
		getRefs(newComponents["responses"], &refs)

		// Filter refs to remove those already in newComponents
		filteredRefs := []string{}
		for _, ref := range refs {
			componentType, componentName := getComponentKey(ref)
			if _, exists := newComponents[componentType][componentName]; !exists {
				filteredRefs = append(filteredRefs, ref)
			}
		}
		refs = filteredRefs
	}

	for key, value := range newComponents {
		components[key] = value
	}

	newPaths := make(map[string]map[string]any)

	for _, pathMethods := range relevantPaths {
		path := pathMethods[0].(string)
		methods := pathMethods[1].([]string)

		if len(methods) == 0 {
			newPaths[path] = api["paths"].(map[string]any)[path].(map[string]any)
		} else {
			newPaths[path] = make(map[string]any)
			for _, method := range methods {
				pathData := api["paths"].(map[string]any)[path].(map[string]any)
				newPaths[path][method] = pathData[method].(map[string]any)
			}
		}
	}

	api["paths"] = make(map[string]any)
	for key, value := range newPaths {
		api["paths"].(map[string]any)[key] = value
	}

	// Write the filtered OpenAPI JSON to a file
	filteredData, err := json.MarshalIndent(api, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	if err := os.WriteFile("./spec/filtered-openapi.json", filteredData, 0644); err != nil {
		fmt.Println("Error writing file:", err)
		return
	}

	fmt.Println("Filtered OpenAPI JSON written to ./spec/filtered-openapi.json")
}
