-- Consolidated final schema for the shipment service, equivalent to the
-- original Flyway migrations V1_1..V1_5 after all of them are applied:
--   V1_1 create tables
--   V1_2 AddressEntity.companyName -> NULL
--   V1_3 AddressEntity.version BIGSERIAL
--   V1_4 OrderItemEntity + quantity + (transient productVariantVersion INT)
--   V1_5 OrderItemEntity replace productVariantVersion INT with
--        productVariantVersionId UUID NOT NULL
-- Unquoted identifiers fold to lowercase on disk (addressentity, postalcode,
-- companyname, externalreference, shipmentmethodid, sentwithid,
-- productvariantversionid, archivedat, ...).

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE AddressEntity (
    id          UUID PRIMARY KEY UNIQUE,
    userId      UUID NULL,
    street1     VARCHAR(255) NOT NULL,
    street2     VARCHAR(255) NOT NULL,
    city        VARCHAR(255) NOT NULL,
    postalCode  VARCHAR(255) NOT NULL,
    country     VARCHAR(255) NOT NULL,
    companyName VARCHAR(255) NULL,
    version     BIGSERIAL
);

CREATE TABLE ProductVariantVersionEntity (
    id     UUID PRIMARY KEY UNIQUE,
    weight DOUBLE PRECISION NOT NULL
);

CREATE TABLE ShipmentMethodEntity (
    id                UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    name              VARCHAR(255) NOT NULL,
    description       VARCHAR(255) NOT NULL,
    externalReference VARCHAR(255) NOT NULL,
    baseFees          INT NOT NULL,
    feesPerItem       INT NOT NULL,
    feesPerKg         INT NOT NULL,
    archivedAt        TIMESTAMPTZ NULL
);

CREATE TABLE ShipmentEntity (
    id                UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    status            VARCHAR(255) NOT NULL,
    shipmentMethodId  UUID NOT NULL,
    shipmentAddressId UUID NOT NULL,
    orderId           UUID NULL,
    returnId          UUID NULL,
    FOREIGN KEY (shipmentMethodId)  REFERENCES ShipmentMethodEntity(id),
    FOREIGN KEY (shipmentAddressId) REFERENCES AddressEntity(id)
);

CREATE TABLE OrderItemEntity (
    id                      UUID PRIMARY KEY UNIQUE,
    sentWithId              UUID NOT NULL,
    productVariantVersionId UUID NOT NULL,
    quantity                INTEGER NOT NULL,
    FOREIGN KEY (sentWithId) REFERENCES ShipmentEntity(id)
);

CREATE TABLE ShipmentToOrderItemEntity (
    id          UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    shipmentId  UUID NOT NULL,
    orderItemId UUID NOT NULL,
    FOREIGN KEY (shipmentId)  REFERENCES ShipmentEntity(id),
    FOREIGN KEY (orderItemId) REFERENCES OrderItemEntity(id)
);
