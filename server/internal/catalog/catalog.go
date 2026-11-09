// Package catalog owns entity types, entities and the document-type taxonomy as data (ATLAS.md §5.4).
// Part 1 only seeds the Core values for a new tenant; packs, entities and the master come later.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/catalog/catalogdb"
)

// StandardEntityType is the one Core entity type with its own meaning: norms and standards cited.
const StandardEntityType = "standard"

type label map[string]string

type entityType struct {
	key    string
	labels label
	// anchor marks types whose shared mentions make two passages candidates for a contradiction (§9.4).
	anchor bool
}

var coreEntityTypes = []entityType{
	{key: "person", labels: label{"es": "Persona", "en": "Person"}},
	{key: "department", labels: label{"es": "Departamento", "en": "Department"}},
	{key: "organization", labels: label{"es": "Organización", "en": "Organization"}},
	{key: StandardEntityType, labels: label{"es": "Norma", "en": "Standard"}, anchor: true},
}

type docType struct {
	key    string
	labels label
}

var coreDocTypes = []docType{
	{"policy", label{"es": "Política", "en": "Policy"}},
	{"procedure", label{"es": "Procedimiento", "en": "Procedure"}},
	{"instruction", label{"es": "Instrucción", "en": "Instruction"}},
	{"manual", label{"es": "Manual", "en": "Manual"}},
	{"form", label{"es": "Formulario", "en": "Form"}},
	{"record", label{"es": "Registro", "en": "Record"}},
	{"report", label{"es": "Informe", "en": "Report"}},
	{"contract", label{"es": "Contrato", "en": "Contract"}},
	{"standard", label{"es": "Norma", "en": "Standard"}},
	{"email", label{"es": "Correo", "en": "Email"}},
	{"other", label{"es": "Otro", "en": "Other"}},
}

// SeedCore inserts the Core entity and document types into the tenant scoped by tx. Safe to run twice.
func SeedCore(ctx context.Context, tx pgx.Tx) error {
	q := catalogdb.New(tx)
	if err := q.InsertEntityTypes(ctx, entityTypeParams()); err != nil {
		return fmt.Errorf("seed entity types: %w", err)
	}
	if err := q.InsertDocTypes(ctx, docTypeParams()); err != nil {
		return fmt.Errorf("seed doc types: %w", err)
	}
	return nil
}

func entityTypeParams() catalogdb.InsertEntityTypesParams {
	var p catalogdb.InsertEntityTypesParams
	for _, t := range coreEntityTypes {
		p.Keys = append(p.Keys, t.key)
		p.Labels = append(p.Labels, mustJSON(t.labels))
		p.Anchors = append(p.Anchors, t.anchor)
	}
	return p
}

func docTypeParams() catalogdb.InsertDocTypesParams {
	var p catalogdb.InsertDocTypesParams
	for _, t := range coreDocTypes {
		p.Keys = append(p.Keys, t.key)
		p.Labels = append(p.Labels, mustJSON(t.labels))
	}
	return p
}

// mustJSON marshals static label maps, which cannot fail.
func mustJSON(l label) []byte {
	b, err := json.Marshal(l)
	if err != nil {
		panic(err)
	}
	return b
}
