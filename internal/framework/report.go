package framework

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type PackListing struct {
	Pack       PackReference `json:"pack"`
	Source     Source        `json:"source"`
	Coverage   Coverage      `json:"coverage"`
	Objectives int           `json:"objectives"`
}

func ListBuiltins() ([]PackListing, error) {
	packs, err := BuiltinPacks()
	if err != nil {
		return nil, err
	}
	listings := make([]PackListing, 0, len(packs))
	for _, pack := range packs {
		listings = append(listings, PackListing{
			Pack:   PackReference{ID: pack.ID, Name: pack.Name, Version: pack.Version, Released: pack.Released, Digest: pack.Digest},
			Source: pack.Source, Coverage: pack.Coverage, Objectives: len(pack.Objectives),
		})
	}
	return listings, nil
}

func WriteJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode framework JSON: %w", err)
	}
	return nil
}

func WritePackListTerminal(writer io.Writer, listings []PackListing) error {
	if _, err := fmt.Fprintf(writer, "Built-in framework packs: %d\n\n", len(listings)); err != nil {
		return err
	}
	for _, listing := range listings {
		if _, err := fmt.Fprintf(writer, "%s @ %s\n", listing.Pack.ID, listing.Pack.Version); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "  %s\n  Evidence: %s; %d technical objectives\n  Provisions: %s\n  Source: %s\n", listing.Pack.Name, listing.Coverage.EvidenceType, listing.Objectives, strings.Join(listing.Coverage.Provisions, ", "), listing.Source.Reference); err != nil {
			return err
		}
		for _, limitation := range listing.Coverage.Limitations {
			if _, err := fmt.Fprintf(writer, "  Limitation: %s\n", limitation); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	return nil
}

func WriteTechnicalEvidenceTerminal(writer io.Writer, report TechnicalEvidenceReport) error {
	if _, err := fmt.Fprintf(writer, "Technical evidence: %s @ %s\n", report.Pack.Name, report.Pack.Version); err != nil {
		return err
	}
	if report.Target != "" {
		if _, err := fmt.Fprintf(writer, "Target: %s\n", report.Target); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "Pack digest: %s\n", report.Pack.Digest); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Source: %s — %s\n\n", report.Source.Reference, report.Source.URL); err != nil {
		return err
	}
	languages := make([]string, len(report.Analysis.Languages))
	for index, language := range report.Analysis.Languages {
		languages[index] = string(language)
	}
	if len(languages) == 0 {
		languages = []string{"none"}
	}
	if _, err := fmt.Fprintf(writer, "Repository graph: %d of %d source files indexed (%s); %d symbols; %d relationships\n",
		report.Analysis.FilesIndexed, report.Analysis.SourceFilesSeen, strings.Join(languages, ", "),
		report.Analysis.SymbolsIndexed, report.Analysis.RelationshipsIndexed); err != nil {
		return err
	}
	if len(report.Analysis.UnsupportedSourceFiles) > 0 {
		if _, err := fmt.Fprintf(writer, "Unsupported source files: %d\n", len(report.Analysis.UnsupportedSourceFiles)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	if len(report.Systems) > 0 {
		systems := make([]string, len(report.Systems))
		for index, system := range report.Systems {
			systems[index] = fmt.Sprintf("%s (%s)", system.Name, system.ID)
		}
		if _, err := fmt.Fprintf(writer, "Declared systems: %s\n\n", strings.Join(systems, ", ")); err != nil {
			return err
		}
	}
	for _, objective := range report.Objectives {
		label := strings.ToUpper(strings.ReplaceAll(string(objective.Status), "-", " "))
		if _, err := fmt.Fprintf(writer, "  %-18s %s — %s (%s)\n", label, objective.ID, objective.Title, objective.SourceReference); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "                       Applicability: %s\n", DescribeApplicability(objective.Applicability)); err != nil {
			return err
		}
		for _, match := range objective.Matches {
			location := match.Path
			if match.StartLine > 0 {
				location = fmt.Sprintf("%s:%d", location, match.StartLine)
			}
			if _, err := fmt.Fprintf(writer, "                       Candidate location: %s\n", location); err != nil {
				return err
			}
			if match.Context.Anchor != nil {
				if _, err := fmt.Fprintf(writer, "                       Anchor: %s (%s)\n", match.Context.Anchor.QualifiedName, match.Context.Anchor.Reachability); err != nil {
					return err
				}
			}
			for _, relationship := range match.Context.Relationships {
				if _, err := fmt.Fprintf(writer, "                       Relationship: %s — %s -> %s", relationship.Kind, relationship.From, relationship.To); err != nil {
					return err
				}
				if relationship.Label != "" {
					if _, err := fmt.Fprintf(writer, " (%s)", relationship.Label); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(writer); err != nil {
					return err
				}
			}
			for _, question := range match.Context.UnresolvedQuestions {
				if _, err := fmt.Fprintf(writer, "                       Unresolved: %s\n", question); err != nil {
					return err
				}
			}
		}
		for _, question := range objective.UnresolvedQuestions {
			if _, err := fmt.Fprintf(writer, "                       Unresolved: %s\n", question); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(writer, "\nTechnical summary: %d checks with candidate evidence, %d with no evidence detected, %d not evaluated\n", report.Summary.CandidateEvidence, report.Summary.NotDetected, report.Summary.NotEvaluated); err != nil {
		return err
	}
	for _, note := range report.Notes {
		if _, err := fmt.Fprintf(writer, "Technical evidence note: %s\n", note); err != nil {
			return err
		}
	}
	for _, limitation := range report.Coverage.Limitations {
		if _, err := fmt.Fprintf(writer, "Coverage limitation: %s\n", limitation); err != nil {
			return err
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(writer, "Technical evidence warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func DescribeApplicability(applicability ObjectiveApplicability) string {
	parts := []string{"legal scope " + applicability.LegalScope}
	if len(applicability.ActivitiesAnyOf) > 0 {
		parts = append(parts, "activities any of "+strings.Join(applicability.ActivitiesAnyOf, ", "))
	}
	if applicability.ExternalUseRequired {
		parts = append(parts, "external use required")
	}
	return strings.Join(parts, "; ")
}
