package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// listArgs carries the common connection parameters (first/skip/order) shared by
// every List* method. Structural predicates, the user filter, and the implicit
// visibility filter are passed separately and combined per-method into the
// combined condition the original BaseConnection built from
// listOfNotNull(predicate, filter, authorizedUserFilter).
type listArgs struct {
	first, skip *int
	order       OrderColumns
	ascending   bool
}

// NewListArgs builds the common connection parameters (the graph layer resolves
// the GraphQL order input to a column set + direction before calling).
func NewListArgs(first, skip *int, order OrderColumns, ascending bool) listArgs {
	return listArgs{first: first, skip: skip, order: order, ascending: ascending}
}

// combineConditions ANDs the non-empty SQL fragments, renumbering nothing (each
// fragment must already reference its own placeholders). Callers pass fragments
// and the merged arg slice built in the same order.
func combineConditions(fragments ...string) string {
	var parts []string
	for _, f := range fragments {
		if f != "" {
			parts = append(parts, f)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		out := parts[0]
		for _, p := range parts[1:] {
			out = out + " AND " + p
		}
		return out
	}
}

// runConnection executes the three connection sub-queries (total, nodes,
// hasNextPage) against a page and scans nodes with scan.
func runConnection[T any](
	ctx context.Context, q querier, p page, scan func(pgx.Rows) ([]T, error),
) (Connection[T], error) {
	total, err := p.totalCount(ctx, q)
	if err != nil {
		return Connection[T]{}, err
	}
	sql, args := p.nodesSQL()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return Connection[T]{}, fmt.Errorf("list %s: %w", p.table, err)
	}
	nodes, err := scan(rows)
	if err != nil {
		return Connection[T]{}, err
	}
	next, err := p.hasNextPage(ctx, q)
	if err != nil {
		return Connection[T]{}, err
	}
	return Connection[T]{Nodes: nodes, TotalCount: total, HasNextPage: next}, nil
}

// visibilityPredicate returns the SQL fragment enforcing publicly-visible rows
// for non-employees on the given table column, or "" for employees (who see
// everything). isEmployee reflects the authorized user's role.
func visibilityPredicate(col string, isEmployee bool) string {
	if isEmployee {
		return ""
	}
	return col + " = TRUE"
}

// ListProducts backs Query.products, Category.products.
//
// structuralWhere/args carry the optional producttocategory predicate (empty
// for the root query). join is "" for the root query or the category join.
// filterVisible is the ProductFilterInput value (nil = no filter). isEmployee
// controls the implicit visibility filter.
func (s *Store) ListProducts(
	ctx context.Context, la listArgs, join, structuralWhere string, structuralArgs []any,
	filterVisible *bool, isEmployee bool,
) (Connection[Product], error) {
	args := append([]any{}, structuralArgs...)
	filterFrag := ""
	if filterVisible != nil {
		args = append(args, *filterVisible)
		filterFrag = fmt.Sprintf("ispubliclyvisible = $%d", len(args))
	}
	where := combineConditions(structuralWhere, filterFrag, visibilityPredicate("ispubliclyvisible", isEmployee))
	p := page{
		table:     "productentity",
		join:      join,
		countCol:  "productentity.id",
		columns:   productProjection,
		where:     where,
		whereArgs: args,
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanProducts)
}

// ListProductVariants backs Product.variants. structuralWhere is always
// "productid = $1"; filterVisible is the ProductVariantFilterInput; isEmployee
// controls the implicit visibility filter (applied on the variant table).
func (s *Store) ListProductVariants(
	ctx context.Context, la listArgs, productID uuid.UUID, filterVisible *bool, isEmployee bool,
) (Connection[ProductVariant], error) {
	args := []any{productID}
	filterFrag := ""
	if filterVisible != nil {
		args = append(args, *filterVisible)
		filterFrag = fmt.Sprintf("ispubliclyvisible = $%d", len(args))
	}
	where := combineConditions("productid = $1", filterFrag, visibilityPredicate("ispubliclyvisible", isEmployee))
	p := page{
		table:     "productvariantentity",
		countCol:  "id",
		columns:   productVariantColumns,
		where:     where,
		whereArgs: args,
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanProductVariants)
}

// ListProductVariantVersions backs ProductVariant.versions (employee-only; no
// visibility filter).
func (s *Store) ListProductVariantVersions(
	ctx context.Context, la listArgs, variantID uuid.UUID,
) (Connection[ProductVariantVersion], error) {
	p := page{
		table:     "productvariantversionentity",
		countCol:  "id",
		columns:   productVariantVersionColumns,
		where:     "productvariantid = $1",
		whereArgs: []any{variantID},
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanProductVariantVersions)
}

// ListCategories backs Query.categories and Product.categories. join is "" for
// the root query or the producttocategory join; structuralWhere is the
// producttocategory predicate (empty for the root query). No visibility filter.
func (s *Store) ListCategories(
	ctx context.Context, la listArgs, join, structuralWhere string, structuralArgs []any,
) (Connection[Category], error) {
	p := page{
		table:     "categoryentity",
		join:      join,
		countCol:  "categoryentity.id",
		columns:   categoryProjection,
		where:     structuralWhere,
		whereArgs: structuralArgs,
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanCategories)
}

// ListCharacteristics backs Category.characteristics (polymorphic nodes).
func (s *Store) ListCharacteristics(
	ctx context.Context, la listArgs, categoryID uuid.UUID,
) (Connection[CategoryCharacteristic], error) {
	p := page{
		table:     "categorycharacteristicentity",
		countCol:  "id",
		columns:   characteristicColumns,
		where:     "categoryid = $1",
		whereArgs: []any{categoryID},
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanCharacteristics)
}

// ListCharacteristicValues backs ProductVariantVersion.characteristicValues
// (polymorphic nodes).
func (s *Store) ListCharacteristicValues(
	ctx context.Context, la listArgs, versionID uuid.UUID,
) (Connection[CategoryCharacteristicValue], error) {
	p := page{
		table:     "categorycharacteristicvalueentity",
		countCol:  "id",
		columns:   characteristicValueColumns,
		where:     "productvariantversionid = $1",
		whereArgs: []any{versionID},
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanCharacteristicValues)
}

// ListMedias backs ProductVariantVersion.medias (join on the media link table).
func (s *Store) ListMedias(
	ctx context.Context, la listArgs, versionID uuid.UUID,
) (Connection[Media], error) {
	p := page{
		table: "mediaentity",
		join: " INNER JOIN productvariantversiontomediaentity ON " +
			"productvariantversiontomediaentity.mediaid = mediaentity.id",
		countCol:  "mediaentity.id",
		columns:   "mediaentity.id",
		where:     "productvariantversiontomediaentity.productvariantversionid = $1",
		whereArgs: []any{versionID},
		orderCols: la.order,
		ascending: la.ascending,
		first:     la.first,
		skip:      la.skip,
	}
	return runConnection(ctx, s.pool, p, scanMedias)
}

// ListCategoricalValues backs CategoricalCategoryCharacteristic.values — a
// projection over DISTINCT stringvalue (not entity rows). totalCount counts
// distinct non-null values; nodes selects DISTINCT stringvalue ordered by the
// VALUE field; hasNextPage probes the same distinct set. No visibility filter.
func (s *Store) ListCategoricalValues(
	ctx context.Context, la listArgs, characteristicID uuid.UUID,
) (Connection[CategoricalValue], error) {
	where := "stringvalue IS NOT NULL AND categorycharacteristicid = $1"

	// totalCount: COUNT(DISTINCT stringvalue).
	var total int
	if err := s.pool.QueryRow(ctx,
		"SELECT COUNT(DISTINCT stringvalue) FROM categorycharacteristicvalueentity WHERE "+where,
		characteristicID,
	).Scan(&total); err != nil {
		return Connection[CategoricalValue]{}, fmt.Errorf("count categorical values: %w", err)
	}

	// nodes: SELECT DISTINCT stringvalue ... ORDER BY stringvalue OFFSET skip LIMIT first.
	dir := "ASC"
	if !la.ascending {
		dir = "DESC"
	}
	skip := 0
	if la.skip != nil {
		skip = *la.skip
	}
	nodeArgs := []any{characteristicID, skip}
	limit := "ALL"
	if la.first != nil {
		nodeArgs = append(nodeArgs, *la.first)
		limit = "$3"
	}
	nodesSQL := fmt.Sprintf(
		"SELECT DISTINCT stringvalue FROM categorycharacteristicvalueentity WHERE %s ORDER BY stringvalue %s OFFSET $2 LIMIT %s",
		where, dir, limit)
	rows, err := s.pool.Query(ctx, nodesSQL, nodeArgs...)
	if err != nil {
		return Connection[CategoricalValue]{}, fmt.Errorf("list categorical values: %w", err)
	}
	nodes, err := scanCategoricalValues(rows, characteristicID)
	if err != nil {
		return Connection[CategoricalValue]{}, err
	}

	// hasNextPage: false when first is null; else EXISTS in the distinct set at
	// OFFSET first+skip (no ORDER BY needed).
	hasNext := false
	if la.first != nil {
		offset := *la.first + skip
		if err := s.pool.QueryRow(ctx, fmt.Sprintf(
			"SELECT EXISTS(SELECT DISTINCT stringvalue FROM categorycharacteristicvalueentity WHERE %s OFFSET $2 LIMIT 1)",
			where), characteristicID, offset,
		).Scan(&hasNext); err != nil {
			return Connection[CategoricalValue]{}, fmt.Errorf("hasNextPage categorical values: %w", err)
		}
	}

	return Connection[CategoricalValue]{Nodes: nodes, TotalCount: total, HasNextPage: hasNext}, nil
}

// Qualified projections for the joined connections so column names stay
// unambiguous; the plain-table connections reuse the unqualified projections.
const (
	productProjection  = "productentity.id, productentity.internalname, productentity.ispubliclyvisible, productentity.defaultvariantid"
	categoryProjection = "categoryentity.id, categoryentity.name, categoryentity.description"
)
