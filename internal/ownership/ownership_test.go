package ownership

import "testing"

func TestResolverDistinguishesAssignedSharedConflictingAndUnassignedPaths(t *testing.T) {
	rules := []Rule{
		{Paths: []string{"services/ranking/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"services/support/**"}, Systems: []string{"support"}},
		{Paths: []string{"shared/models/**"}, Systems: []string{"ranking", "support"}},
		{Paths: []string{"conflict/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"conflict/special/**"}, Systems: []string{"support"}},
	}
	if err := Validate(rules, []string{"ranking", "support"}); err != nil {
		t.Fatal(err)
	}
	resolver := New(rules)
	tests := []struct {
		path    string
		status  Status
		systems string
	}{
		{path: "services/ranking/api.go", status: StatusAssigned, systems: "ranking"},
		{path: "services/support/api.py", status: StatusAssigned, systems: "support"},
		{path: "shared/models/client.ts", status: StatusShared, systems: "ranking,support"},
		{path: "conflict/special/model.go", status: StatusConflicting, systems: "ranking,support"},
		{path: "README.md", status: StatusUnassigned, systems: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			result := resolver.Resolve(testCase.path)
			if result.Status != testCase.status || join(result.Systems) != testCase.systems {
				t.Fatalf("resolution = %#v", result)
			}
		})
	}
}

func TestValidateRejectsUnsafeOrUnknownOwnership(t *testing.T) {
	tests := []Rule{
		{Paths: []string{"../outside/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"/absolute/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"valid/**"}, Systems: []string{"unknown"}},
		{Paths: []string{"valid/**", "valid/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"valid/**"}, Systems: []string{"ranking", "ranking"}},
	}
	for _, rule := range tests {
		if err := Validate([]Rule{rule}, []string{"ranking"}); err == nil {
			t.Fatalf("expected invalid rule: %#v", rule)
		}
	}
}

func join(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}
