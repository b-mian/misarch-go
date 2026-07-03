package graph

import (
	"misarch/review/store"
)

// This file maps store documents to GraphQL models, the ReviewOrderInput to a
// store sort spec, and the Rating enum to/from its stored string form.
//
// Relationship fields (Review.User, Review.ProductVariant, and the various
// reviews/averageRating fields) are intentionally left to their own field
// resolvers; the mapper only carries the FK extraFields (UserID,
// ProductVariantID, ProductID) those resolvers need.

// toReview maps a store.Review to the GraphQL model. On a rating that fails to
// map (e.g. a legacy typo'd string that even the tolerant reader rejects),
// toReview returns an error so the field surfaces a GraphQL error, matching the
// original where such a document fails to deserialize.
func toReview(r store.Review) (*Review, error) {
	rating, err := ratingFromStored(r.Rating)
	if err != nil {
		return nil, err
	}
	return &Review{
		ID:               r.ID,
		Body:             r.Body,
		Rating:           rating,
		CreatedAt:        r.CreatedAt,
		LastUpdatedAt:    r.LastUpdatedAt,
		IsVisible:        r.IsVisible,
		UserID:           r.User.ID,
		ProductVariantID: r.ProductVariant.ID,
		ProductID:        r.ProductVariant.ProductID,
	}, nil
}

// toReviewConnection maps a paginated store result to the GraphQL connection.
// nodes is never nil (empty slice on no matches, matching the SDL [Review!]!).
func toReviewConnection(c store.Connection[store.Review]) (*ReviewConnection, error) {
	nodes := make([]Review, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		rv, err := toReview(n)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *rv)
	}
	return &ReviewConnection{
		Nodes:       nodes,
		HasNextPage: c.HasNextPage,
		TotalCount:  c.TotalCount,
	}, nil
}

// reviewSort resolves a ReviewOrderInput to a store sort spec, replicating the
// original defaults (direction ASC, field ID) and the deliberately non-1:1
// field→Mongo-key mapping (notably CREATED_AT → last_updated_at, USER_ID →
// user, PRODUCT_VARIANT → product_variant).
func reviewSort(in *ReviewOrderInput) store.SortSpec {
	direction := 1 // ASC default
	field := ReviewOrderFieldID
	if in != nil {
		if in.Direction != nil && *in.Direction == OrderDirectionDesc {
			direction = -1
		}
		if in.Field != nil {
			field = *in.Field
		}
	}
	return store.SortSpec{Key: orderFieldKey(field), Direction: direction}
}

// orderFieldKey maps a ReviewOrderField enum value to its Mongo sort key,
// implementing every enum value exactly per the spec's mapping table.
func orderFieldKey(f ReviewOrderField) string {
	switch f {
	case ReviewOrderFieldID:
		return "_id"
	case ReviewOrderFieldUserID:
		return "user"
	case ReviewOrderFieldProductVariant:
		return "product_variant"
	case ReviewOrderFieldRating:
		return "rating"
	case ReviewOrderFieldCreatedAt:
		return "last_updated_at"
	default:
		return "_id"
	}
}

// ratingToStored maps a wire Rating enum to the string persisted in Mongo.
//
// The original Rust service had a bug: create wrote the serde form ("FourStars")
// while update wrote a typo ("FourStarst"), and reads of the typo'd string
// failed. Per the spec's default recommendation, the Go port stores the
// consistent non-typo form for BOTH create and update so a 4-star review
// round-trips everywhere. Flagged as a deviation.
func ratingToStored(r Rating) string {
	switch r {
	case RatingOneStars:
		return "OneStars"
	case RatingTwoStars:
		return "TwoStars"
	case RatingThreeStars:
		return "ThreeStars"
	case RatingFourStars:
		return "FourStars"
	case RatingFiveStars:
		return "FiveStars"
	default:
		return string(r)
	}
}

// ratingFromStored maps a stored rating string back to the wire enum. It reads
// tolerantly: the legacy typo form "FourStarst" is also accepted as 4 stars so
// documents written by the original Rust service still deserialize.
func ratingFromStored(s string) (Rating, error) {
	switch s {
	case "OneStars":
		return RatingOneStars, nil
	case "TwoStars":
		return RatingTwoStars, nil
	case "ThreeStars":
		return RatingThreeStars, nil
	case "FourStars", "FourStarst":
		return RatingFourStars, nil
	case "FiveStars":
		return RatingFiveStars, nil
	default:
		return "", &badRatingError{value: s}
	}
}

// ratingDiscriminant returns the numeric 1..5 value of a Rating, used by
// averageRating (the Rust enum discriminant OneStars=1 … FiveStars=5).
func ratingDiscriminant(r Rating) int {
	switch r {
	case RatingOneStars:
		return 1
	case RatingTwoStars:
		return 2
	case RatingThreeStars:
		return 3
	case RatingFourStars:
		return 4
	case RatingFiveStars:
		return 5
	default:
		return 0
	}
}
