package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// upsertCharacteristicValues validates and upserts the characteristic values of
// one product-variant-version, reproducing CategoryCharacteristicValueService
// exactly:
//
//  1. Empty (both lists) → skip everything (no validation, no writes).
//  2. Combined id list (categorical then numerical); any duplicate → error.
//  3. Query the characteristics valid for this version (belong to a category of
//     the version's product).
//  4. Any requested id not in the valid set → error.
//  5. Each valid characteristic's discriminator must match the list it came
//     from → else error.
//  6. Upsert each value (ON CONFLICT DO UPDATE).
func upsertCharacteristicValues(
	ctx context.Context, tx pgx.Tx, versionID uuid.UUID,
	categorical []CharacteristicValueInput, numerical []NumericalValueInput,
) error {
	categoricalIDs := make([]uuid.UUID, len(categorical))
	for i, c := range categorical {
		categoricalIDs[i] = c.CharacteristicID
	}
	numericalIDs := make([]uuid.UUID, len(numerical))
	for i, n := range numerical {
		numericalIDs[i] = n.CharacteristicID
	}

	// combined list = categorical ids then numerical ids.
	all := append(append([]uuid.UUID{}, categoricalIDs...), numericalIDs...)
	if len(all) == 0 {
		return nil
	}

	// 2. Duplicates (within or across lists). Kotlin's duplicates() returns a
	// Set (first-encounter order) rendered as "[a, b]".
	if dups := duplicateUUIDs(all); len(dups) > 0 {
		return fmt.Errorf("Duplicate characteristic ids: %s", formatUUIDSet(dups))
	}

	// 3. Valid characteristics for this version.
	valid, err := findValidCharacteristics(ctx, tx, versionID, all)
	if err != nil {
		return err
	}
	validSet := make(map[uuid.UUID]string, len(valid)) // id -> discriminator
	for _, v := range valid {
		validSet[v.id] = v.discriminator
	}

	// 4. invalid = requested − valid (preserve first-occurrence order of the
	// requested list for the message, matching a Kotlin LinkedHashSet difference).
	var invalid []uuid.UUID
	seenInvalid := make(map[uuid.UUID]struct{})
	for _, id := range all {
		if _, ok := validSet[id]; ok {
			continue
		}
		if _, done := seenInvalid[id]; done {
			continue
		}
		seenInvalid[id] = struct{}{}
		invalid = append(invalid, id)
	}
	if len(invalid) > 0 {
		return fmt.Errorf("Invalid characteristic ids which cannot be used here: %s", formatUUIDSet(invalid))
	}

	// 5. Discriminator of each valid characteristic must match its source list.
	categoricalMembership := membership(categoricalIDs)
	numericalMembership := membership(numericalIDs)
	for _, v := range valid {
		var ok bool
		switch v.discriminator {
		case DiscriminatorCategorical:
			_, ok = categoricalMembership[v.id]
		case DiscriminatorNumerical:
			_, ok = numericalMembership[v.id]
		}
		if !ok {
			return fmt.Errorf("Characteristic id %s is not valid for this type of characteristic.", v.id)
		}
	}

	// 6. Upsert.
	for _, c := range categorical {
		if err := upsertValue(ctx, tx, c.CharacteristicID, versionID, &c.Value, nil, DiscriminatorCategorical); err != nil {
			return err
		}
	}
	for _, n := range numerical {
		val := n.Value
		if err := upsertValue(ctx, tx, n.CharacteristicID, versionID, nil, &val, DiscriminatorNumerical); err != nil {
			return err
		}
	}
	return nil
}

// validCharacteristic is a row of findValidCharacteristics (id + discriminator).
type validCharacteristic struct {
	id            uuid.UUID
	discriminator string
}

// findValidCharacteristics returns the characteristics among ids that are
// compatible with the version — i.e. belong to a category of the version's
// product. Mirrors CategoryCharacteristicValueRepository.findValidCategoryCharacteristics.
// ids must be non-empty (guaranteed by the caller).
func findValidCharacteristics(
	ctx context.Context, tx pgx.Tx, versionID uuid.UUID, ids []uuid.UUID,
) ([]validCharacteristic, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT cc.id, cc.discriminator
		FROM productvariantversionentity pvv
		JOIN productvariantentity pv ON pvv.productvariantid = pv.id
		JOIN productentity p ON pv.productid = p.id
		JOIN producttocategoryentity ptc ON p.id = ptc.productid
		JOIN categoryentity c ON ptc.categoryid = c.id
		JOIN categorycharacteristicentity cc ON c.id = cc.categoryid
		WHERE pvv.id = $1 AND cc.id = ANY($2)`,
		versionID, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("find valid characteristics: %w", err)
	}
	defer rows.Close()
	var out []validCharacteristic
	for rows.Next() {
		var vc validCharacteristic
		if err := rows.Scan(&vc.id, &vc.discriminator); err != nil {
			return nil, fmt.Errorf("scan valid characteristic: %w", err)
		}
		out = append(out, vc)
	}
	return out, rows.Err()
}

// upsertValue upserts one characteristic value.
func upsertValue(
	ctx context.Context, tx pgx.Tx, characteristicID, versionID uuid.UUID,
	stringValue *string, doubleValue *float64, discriminator string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO categorycharacteristicvalueentity
		(categorycharacteristicid, productvariantversionid, stringvalue, doublevalue, discriminator)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (categorycharacteristicid, productvariantversionid)
		DO UPDATE SET stringvalue = $3, doublevalue = $4`,
		characteristicID, versionID, stringValue, doubleValue, discriminator,
	)
	if err != nil {
		return fmt.Errorf("upsert characteristic value: %w", err)
	}
	return nil
}

// duplicateUUIDs returns the ids that occur more than once, in first-encounter
// order (mirrors Kotlin Iterable.duplicates()).
func duplicateUUIDs(ids []uuid.UUID) []uuid.UUID {
	counts := make(map[uuid.UUID]int, len(ids))
	order := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if counts[id] == 0 {
			order = append(order, id)
		}
		counts[id]++
	}
	var dups []uuid.UUID
	for _, id := range order {
		if counts[id] > 1 {
			dups = append(dups, id)
		}
	}
	return dups
}

// membership builds a set from a slice.
func membership(ids []uuid.UUID) map[uuid.UUID]struct{} {
	m := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// formatUUIDSet renders a UUID slice the way Kotlin renders a Set/List in an
// exception message: "[a, b, c]" (or "[]" when empty).
func formatUUIDSet(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
