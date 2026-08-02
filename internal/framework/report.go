package framework

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type PackListing struct {
	Pack     PackReference `json:"pack"`
	Source   Source        `json:"source"`
	Coverage Coverage      `json:"coverage"`
	Controls int           `json:"controls"`
}

func ListBuiltins() ([]PackListing, error) {
	packs, err := BuiltinPacks()
	if err != nil {
		return nil, err
	}
	listings := make([]PackListing, 0, len(packs))
	for _, pack := range packs {
		listings = append(listings, PackListing{
			Pack:   PackReference{ID: pack.ID, Name: pack.Name, Version: pack.Version, Released: pack.Released},
			Source: pack.Source, Coverage: pack.Coverage, Controls: len(pack.Controls),
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
		if _, err := fmt.Fprintf(writer, "  %s\n  Coverage: %s; roles %s; %d controls\n  Source: %s\n", listing.Pack.Name, listing.Coverage.RiskClassification, strings.Join(listing.Coverage.Roles, ", "), listing.Controls, listing.Source.Reference); err != nil {
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

func WriteAssessmentTerminal(writer io.Writer, report AssessmentReport) error {
	if _, err := fmt.Fprintf(writer, "Framework assessment: %s @ %s\n", report.Pack.Name, report.Pack.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Source: %s — %s\n\n", report.Source.Reference, report.Source.URL); err != nil {
		return err
	}
	for _, system := range report.Systems {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n  Pack activation: %s\n", system.SystemName, system.SystemID, system.Activation); err != nil {
			return err
		}
		for _, reason := range system.ActivationReasons {
			if _, err := fmt.Fprintf(writer, "  Activation note: %s\n", reason); err != nil {
				return err
			}
		}
		for _, control := range system.Controls {
			if _, err := fmt.Fprintf(writer, "  %-14s %s — %s (%s)\n", strings.ToUpper(string(control.Status)), control.ID, control.Title, control.SourceReference); err != nil {
				return err
			}
			if control.ApplicabilityNote != "" {
				if _, err := fmt.Fprintf(writer, "                 Applicability: %s\n", control.ApplicabilityNote); err != nil {
					return err
				}
			}
			for _, evidence := range control.EvidenceRequirements {
				if evidence.Status == EvidenceMissing {
					if _, err := fmt.Fprintf(writer, "                 Missing: %s\n", evidence.Description); err != nil {
						return err
					}
					continue
				}
				paths := make([]string, len(evidence.Matches))
				for index, match := range evidence.Matches {
					paths[index] = match.Path
				}
				if _, err := fmt.Fprintf(writer, "                 Candidate: %s [%s]\n", evidence.Description, strings.Join(paths, ", ")); err != nil {
					return err
				}
			}
		}
		if system.Summary.Total > 0 {
			if _, err := fmt.Fprintf(writer, "  Control summary: %d missing, %d partial, %d evidence-found\n", system.Summary.Missing, system.Summary.Partial, system.Summary.EvidenceFound); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	for _, note := range report.Notes {
		if _, err := fmt.Fprintf(writer, "Framework note: %s\n", note); err != nil {
			return err
		}
	}
	for _, limitation := range report.Coverage.Limitations {
		if _, err := fmt.Fprintf(writer, "Coverage limitation: %s\n", limitation); err != nil {
			return err
		}
	}
	return nil
}
