package patterns

import (
    "testing"
)

func TestRegistryRegistersDefaultPatterns(t *testing.T) {
    r := GetRegistry()
    // Expect core patterns to be present
    types := []PatternType{PatternReflection, PatternReact, PatternChainOfThought, PatternDebate, PatternTreeOfThoughts}
    for _, pt := range types {
        if _, ok := r.Get(pt); !ok {
            t.Fatalf("expected pattern %s to be registered", pt)
        }
    }
}

func TestRegistrySelectorHonorsContextHint(t *testing.T) {
    r := GetRegistry()
    ctx := map[string]interface{}{"pattern": string(PatternReact)}
    p, err := r.SelectForTask("test query", ctx)
    if err != nil {
        t.Fatalf("SelectForTask returned error: %v", err)
    }
    if p.GetType() != PatternReact {
        t.Fatalf("expected react pattern, got %s", p.GetType())
    }
}

