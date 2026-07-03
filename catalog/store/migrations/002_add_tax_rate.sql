ALTER TABLE ProductVariantVersionEntity
    ADD COLUMN taxRateId UUID;

CREATE TABLE TaxRateEntity (
    id UUID PRIMARY KEY UNIQUE
);

ALTER TABLE ProductVariantVersionEntity
ADD CONSTRAINT fk_tax_rate_od
FOREIGN KEY (taxRateId) REFERENCES TaxRateEntity(id);
