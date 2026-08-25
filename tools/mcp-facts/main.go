package main

import (
	"fmt"

	"github.com/neprel/git-a2a/internal/cli"
)

func mark(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func main() {
	fmt.Println("<!-- Code generated from internal/cli MCP registration; DO NOT EDIT. -->")
	fmt.Println("# MCP tool text audit")
	fmt.Println()
	fmt.Println("This table is generated from the same facts used to register `tools/list`. `make docs-check`")
	fmt.Println("fails if the checked-in table differs. Default access exposes exactly eight tools;")
	fmt.Println("`--allow-write` adds six tools.")
	fmt.Println()
	fmt.Println("| Access | Tool | Description | readOnly | destructive | idempotent | openWorld |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, fact := range cli.MCPToolFacts() {
		fmt.Printf("| `%s` | `%s` | %s | %s | %s | %s | %s |\n",
			fact.Access, fact.Name, fact.Description, mark(fact.ReadOnly), mark(fact.Destructive),
			mark(fact.Idempotent), mark(fact.OpenWorld))
	}
}
