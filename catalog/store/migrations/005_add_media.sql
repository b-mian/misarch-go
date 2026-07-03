CREATE TABLE MediaEntity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE ProductVariantVersionToMediaEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    productVariantVersionId UUID,
    mediaId UUID,
    FOREIGN KEY (productVariantVersionId) REFERENCES ProductVariantVersionEntity(id),
    FOREIGN KEY (mediaId) REFERENCES MediaEntity(id),
    UNIQUE (productVariantVersionId, mediaId)
);
