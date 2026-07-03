CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE UserEntity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE AddressEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    street1 VARCHAR(255) NOT NULL,
    street2 VARCHAR(255) NULL,
    city VARCHAR(255) NOT NULL,
    postalCode VARCHAR(255) NOT NULL,
    country VARCHAR(255) NOT NULL,
    companyName VARCHAR(255) NULL,
    userId UUID NULL,
    archivedAt TIMESTAMPTZ NULL,
    version BIGSERIAL,
    FOREIGN KEY (userId) REFERENCES UserEntity(id)
);
