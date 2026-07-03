CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE TaxRateEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description VARCHAR(255) NOT NULL,
    currentVersionId UUID NULL
);

CREATE TABLE TaxRateVersionEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    rate DOUBLE PRECISION NOT NULL,
    createdAt TIMESTAMPTZ NOT NULL,
    version INT NOT NULL,
    taxRateId UUID,
    FOREIGN KEY (taxRateId) REFERENCES TaxRateEntity(id)
);

ALTER TABLE TaxRateEntity
ADD CONSTRAINT fk_tax_rate_version
FOREIGN KEY (currentVersionId) REFERENCES TaxRateVersionEntity(id);
