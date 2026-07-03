CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE OrderEntity (
    id UUID PRIMARY KEY UNIQUE,
    userId UUID NOT NULL
);

CREATE TABLE ReturnEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    reason VARCHAR(255) NOT NULL,
    orderId UUID NOT NULL,
    refundedAmount BIGINT NOT NULL,
    createdAt TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (orderId) REFERENCES OrderEntity(id)
);

CREATE TABLE OrderItemEntity (
    id UUID PRIMARY KEY UNIQUE,
    returnedWithId UUID NULL,
    sentWithId UUID NULL,
    orderId UUID NOT NULL,
    compensatableAmount BIGINT NULL,
    productVariantVersionId UUID NULL,
    FOREIGN KEY (orderId) REFERENCES OrderEntity(id)
);

CREATE TABLE ProductVariantVersionEntity (
    id UUID PRIMARY KEY UNIQUE,
    canBeReturnedForDays INT NULL
);

CREATE TABLE ShipmentEntity (
    id UUID PRIMARY KEY UNIQUE,
    deliveredAt TIMESTAMPTZ NULL
);

CREATE OR REPLACE FUNCTION check_return_order_match()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.returnedWithId IS NOT NULL THEN
        IF NOT EXISTS (
            SELECT 1 FROM OrderItemEntity order_item_entity
            JOIN ReturnEntity return_entity ON order_item_entity.orderId = return_entity.orderId
            WHERE order_item_entity.id = NEW.id AND return_entity.id = NEW.returnedWithId
        ) THEN
            RAISE EXCEPTION 'Returned order ID does not match the order ID of the return';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER before_update_orderitem_check_return_order_match
BEFORE UPDATE ON OrderItemEntity
FOR EACH ROW
EXECUTE FUNCTION check_return_order_match();


CREATE OR REPLACE FUNCTION check_return_within_period()
RETURNS TRIGGER AS $$
DECLARE
    created_at_date TIMESTAMPTZ;
    delivered_at_date TIMESTAMPTZ;
    return_days_limit INT;
    days_between INT;
BEGIN
    SELECT createdAt INTO created_at_date
    FROM ReturnEntity
    WHERE id = NEW.returnedWithId;

    SELECT ShipmentEntity.deliveredAt, ProductVariantVersionEntity.canBeReturnedForDays INTO delivered_at_date, return_days_limit
    FROM OrderItemEntity
    JOIN ProductVariantVersionEntity ON OrderItemEntity.productVariantVersionId = ProductVariantVersionEntity.id
    JOIN ShipmentEntity ON OrderItemEntity.sentWithId = ShipmentEntity.id
    WHERE OrderItemEntity.id = NEW.id;

    IF delivered_at_date IS NULL THEN
        RAISE EXCEPTION 'Shipment has not been delivered yet.';
    END IF;

    days_between := DATE_PART('day', created_at_date - delivered_at_date);

    IF return_days_limit IS NOT NULL AND days_between > return_days_limit THEN
        RAISE EXCEPTION 'Return period of % days has been exceeded.', return_days_limit;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER check_return_trigger
BEFORE UPDATE OF returnedWithId ON OrderItemEntity
FOR EACH ROW
EXECUTE FUNCTION check_return_within_period();
