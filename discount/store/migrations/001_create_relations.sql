-- Discount service schema, ported from the original Flyway migration
-- V1_1__create_relations.sql. Unquoted identifiers fold to lowercase on disk,
-- so every table/column here is written lowercase and referenced lowercase
-- throughout the store (`discountid`, `maxusagesperuser`, ...). The original
-- DDL had a stray `)` after the DiscountUsageEntity table; it is omitted here
-- (a deliberate correction with no externally observable difference — the table
-- exists either way). All UNIQUE constraints, foreign keys, and both business
-- triggers are reproduced exactly; the trigger error strings are a contract.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- id-only replicas of foreign entities (populated from Dapr */created events)
CREATE TABLE userentity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE productentity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE productvariantentity (
    id UUID PRIMARY KEY UNIQUE,
    productid UUID NOT NULL          -- no FK to productentity
);

CREATE TABLE categoryentity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE producttocategoryentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    productid UUID NOT NULL,          -- no FK
    categoryid UUID NOT NULL,         -- no FK
    UNIQUE (productid, categoryid)
);

-- owned entities
CREATE TABLE discountentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    discount DOUBLE PRECISION NOT NULL,
    maxusagesperuser INTEGER NULL,
    validfrom TIMESTAMPTZ NOT NULL,
    validuntil TIMESTAMPTZ NOT NULL,
    minorderamount INTEGER NULL
);

CREATE TABLE couponentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    usages INTEGER NOT NULL DEFAULT 0,
    maxusages INTEGER NULL,
    validfrom TIMESTAMPTZ NOT NULL,
    validuntil TIMESTAMPTZ NOT NULL,
    code VARCHAR(255) NOT NULL,
    discountid UUID NOT NULL,
    FOREIGN KEY (discountid) REFERENCES discountentity(id),
    UNIQUE (code)
);

CREATE TABLE couponredemptionentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    couponid UUID NOT NULL,
    userid UUID NOT NULL,
    FOREIGN KEY (couponid) REFERENCES couponentity(id),
    FOREIGN KEY (userid) REFERENCES userentity(id),
    UNIQUE (couponid, userid)
);

CREATE TABLE discounttoproductentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    discountid UUID NOT NULL,
    productid UUID NOT NULL,
    FOREIGN KEY (discountid) REFERENCES discountentity(id),
    FOREIGN KEY (productid) REFERENCES productentity(id),
    UNIQUE (discountid, productid)
);

CREATE TABLE discounttocategoryentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    discountid UUID NOT NULL,
    categoryid UUID NOT NULL,
    FOREIGN KEY (discountid) REFERENCES discountentity(id),
    FOREIGN KEY (categoryid) REFERENCES categoryentity(id),
    UNIQUE (discountid, categoryid)
);

CREATE TABLE discounttoproductvariantentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    discountid UUID NOT NULL,
    productvariantid UUID NOT NULL,
    FOREIGN KEY (discountid) REFERENCES discountentity(id),
    FOREIGN KEY (productvariantid) REFERENCES productvariantentity(id),
    UNIQUE (discountid, productvariantid)
);

CREATE TABLE discountusageentity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    discountid UUID NOT NULL,
    userid UUID NOT NULL,
    usages BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (discountid) REFERENCES discountentity(id),
    FOREIGN KEY (userid) REFERENCES userentity(id),
    UNIQUE (discountid, userid)
);

-- Registering a coupon (inserting a redemption) atomically increments
-- couponentity.usages and enforces the global maxusages cap in the DB.
CREATE OR REPLACE FUNCTION update_coupon_usages()
RETURNS TRIGGER AS $$
DECLARE
    current_usages INTEGER;
    max_usages INTEGER;
BEGIN
    SELECT couponentity.usages, couponentity.maxusages
    INTO current_usages, max_usages
    FROM couponentity couponentity
    WHERE couponentity.id = NEW.couponid;

    current_usages := current_usages + 1;

    IF max_usages IS NOT NULL AND current_usages > max_usages THEN
        RAISE EXCEPTION 'Coupon has been used too often';
    END IF;

    UPDATE couponentity
    SET usages = current_usages
    WHERE id = NEW.couponid;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER before_insert_update_coupon_usages
BEFORE INSERT ON couponredemptionentity
FOR EACH ROW
EXECUTE FUNCTION update_coupon_usages();

-- Second line of defense on the per-user discount cap, in addition to the
-- application check in validateOrder.
CREATE OR REPLACE FUNCTION check_discount_usages()
RETURNS TRIGGER AS $$
DECLARE
    max_usages_per_user INTEGER;
BEGIN
    IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND NEW.usages > OLD.usages) THEN
        SELECT maxusagesperuser
        INTO max_usages_per_user
        FROM discountentity
        WHERE id = NEW.discountid;

        IF max_usages_per_user IS NOT NULL AND max_usages_per_user < NEW.usages THEN
            RAISE EXCEPTION 'The user has applied the discount too often';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER check_discount_usages_trigger
BEFORE INSERT OR UPDATE OF usages ON discountusageentity
FOR EACH ROW
EXECUTE FUNCTION check_discount_usages();
