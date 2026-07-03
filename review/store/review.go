package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrReviewAlreadyExists is returned by CreateReview when the (user, variant)
// pair already has a review. The resolver renders the exact user-facing message.
var ErrReviewAlreadyExists = errors.New("review already exists")

// SortSpec is a resolved Mongo sort: the document key and direction (1 or -1),
// mapped by the graph layer from the ReviewOrderInput exactly per the spec's
// field→key table (note CREATED_AT → last_updated_at).
type SortSpec struct {
	Key       string
	Direction int
}

// Page holds the pagination arguments as the resolvers receive them: first is
// the optional limit, skip the optional offset. Nil means unset.
type Page struct {
	First *int
	Skip  *int
}

// listReviews runs the shared pagination against the reviews collection with an
// arbitrary filter. It mirrors the original mongodb-cursor-pagination semantics:
// totalCount is a full count of the filter ignoring skip/limit, nodes is the
// page after skip+limit+sort, and hasNextPage = skip + len(nodes) < totalCount.
// nodes is never nil (empty slice on no matches).
func (s *Store) listReviews(ctx context.Context, filter bson.M, sort SortSpec, page Page) (Connection[Review], error) {
	total, err := s.reviews.CountDocuments(ctx, filter)
	if err != nil {
		return Connection[Review]{}, err
	}

	findOpts := options.Find().SetSort(bson.D{{Key: sort.Key, Value: sort.Direction}})
	if page.Skip != nil {
		findOpts.SetSkip(int64(*page.Skip))
	}
	if page.First != nil {
		findOpts.SetLimit(int64(*page.First))
	}

	cursor, err := s.reviews.Find(ctx, filter, findOpts)
	if err != nil {
		return Connection[Review]{}, err
	}
	nodes := []Review{}
	if err := cursor.All(ctx, &nodes); err != nil {
		return Connection[Review]{}, err
	}

	skip := 0
	if page.Skip != nil {
		skip = *page.Skip
	}
	hasNextPage := int64(skip+len(nodes)) < total

	return Connection[Review]{
		Nodes:       nodes,
		TotalCount:  int(total),
		HasNextPage: hasNextPage,
	}, nil
}

// ListReviews returns a page of all reviews (no filter), matching Query.reviews.
func (s *Store) ListReviews(ctx context.Context, sort SortSpec, page Page) (Connection[Review], error) {
	return s.listReviews(ctx, bson.M{}, sort, page)
}

// ListReviewsByUser returns a page of reviews for a user (filter user._id).
func (s *Store) ListReviewsByUser(ctx context.Context, userID uuid.UUID, sort SortSpec, page Page) (Connection[Review], error) {
	return s.listReviews(ctx, bson.M{"user._id": userID}, sort, page)
}

// ListReviewsByProductVariant returns a page of reviews for a product variant
// (filter product_variant._id).
func (s *Store) ListReviewsByProductVariant(ctx context.Context, variantID uuid.UUID, sort SortSpec, page Page) (Connection[Review], error) {
	return s.listReviews(ctx, bson.M{"product_variant._id": variantID}, sort, page)
}

// ListReviewsByProduct returns a page of reviews for a product, joining through
// the product_id embedded in each review's product_variant.
func (s *Store) ListReviewsByProduct(ctx context.Context, productID uuid.UUID, sort SortSpec, page Page) (Connection[Review], error) {
	return s.listReviews(ctx, bson.M{"product_variant.product_id": productID}, sort, page)
}

// AllReviewsByProduct fetches every review for a product with no paging, used
// by averageRating. The sort is irrelevant for an average; ID ascending mirrors
// the Rust call self.reviews(ctx, None, None, None).
func (s *Store) AllReviewsByProduct(ctx context.Context, productID uuid.UUID) ([]Review, error) {
	c, err := s.listReviews(ctx, bson.M{"product_variant.product_id": productID}, SortSpec{Key: "_id", Direction: 1}, Page{})
	if err != nil {
		return nil, err
	}
	return c.Nodes, nil
}

// AllReviewsByProductVariant fetches every review for a product variant with no
// paging, used by averageRating.
func (s *Store) AllReviewsByProductVariant(ctx context.Context, variantID uuid.UUID) ([]Review, error) {
	c, err := s.listReviews(ctx, bson.M{"product_variant._id": variantID}, SortSpec{Key: "_id", Direction: 1}, Page{})
	if err != nil {
		return nil, err
	}
	return c.Nodes, nil
}

// FindReview loads a review by id. The bool is false when no document matches
// (the resolver decides whether that is a null or an error). A transport error
// is returned as err.
func (s *Store) FindReview(ctx context.Context, id uuid.UUID) (Review, bool, error) {
	var r Review
	err := s.reviews.FindOne(ctx, bson.M{"_id": id}).Decode(&r)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Review{}, false, nil
	}
	if err != nil {
		return Review{}, false, err
	}
	return r, true, nil
}

// UserExists reports whether a user shadow document exists.
func (s *Store) UserExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return exists(ctx, s.users, id)
}

// ProductVariantExists reports whether a product variant shadow document exists.
func (s *Store) ProductVariantExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return exists(ctx, s.productVariants, id)
}

// FindUser loads a user shadow document by id. found is false on a miss; a
// transport error is returned as err (entity resolvers map it to a message).
func (s *Store) FindUser(ctx context.Context, id uuid.UUID) (User, bool, error) {
	var u User
	err := s.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return u, true, nil
}

// FindProduct loads a product shadow document by id.
func (s *Store) FindProduct(ctx context.Context, id uuid.UUID) (Product, bool, error) {
	var p Product
	err := s.products.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Product{}, false, nil
	}
	if err != nil {
		return Product{}, false, err
	}
	return p, true, nil
}

// GetProductVariant loads the full product variant shadow document (id +
// product_id), used to embed a copy into a new review.
func (s *Store) GetProductVariant(ctx context.Context, id uuid.UUID) (ProductVariant, bool, error) {
	var pv ProductVariant
	err := s.productVariants.FindOne(ctx, bson.M{"_id": id}).Decode(&pv)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ProductVariant{}, false, nil
	}
	if err != nil {
		return ProductVariant{}, false, err
	}
	return pv, true, nil
}

// reviewExistsForPair reports whether the (user, variant) pair already has a
// review, matching the pre-insert uniqueness probe.
func (s *Store) reviewExistsForPair(ctx context.Context, userID, variantID uuid.UUID) (bool, error) {
	err := s.reviews.FindOne(ctx, bson.M{
		"product_variant._id": variantID,
		"user._id":            userID,
	}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertReview performs the uniqueness check then inserts the review, returning
// the re-fetched document (matching insert_one → re-fetch by inserted id).
// ErrReviewAlreadyExists is returned when the pair already has a review; the
// error is returned before any write so no duplicate is inserted.
func (s *Store) InsertReview(ctx context.Context, r Review) (Review, error) {
	dup, err := s.reviewExistsForPair(ctx, r.User.ID, r.ProductVariant.ID)
	if err != nil {
		return Review{}, ErrReviewAlreadyExists
	}
	if dup {
		return Review{}, ErrReviewAlreadyExists
	}
	if _, err := s.reviews.InsertOne(ctx, r); err != nil {
		return Review{}, err
	}
	got, _, err := s.FindReview(ctx, r.ID)
	if err != nil {
		return Review{}, err
	}
	return got, nil
}

// SetReviewField applies a single {$set: {field: value, last_updated_at: ts}}
// update to one review, mirroring the original per-field update_one calls (each
// provided field is its own round-trip and bumps last_updated_at).
func (s *Store) SetReviewField(ctx context.Context, id uuid.UUID, field string, value any, ts DateTime) error {
	_, err := s.reviews.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{field: value, "last_updated_at": ts}},
	)
	return err
}

// DeleteReview hard-deletes a review by id.
func (s *Store) DeleteReview(ctx context.Context, id uuid.UUID) error {
	_, err := s.reviews.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// exists is a shared existence probe by _id for the shadow collections.
func exists(ctx context.Context, coll *mongo.Collection, id uuid.UUID) (bool, error) {
	err := coll.FindOne(ctx, bson.M{"_id": id}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
