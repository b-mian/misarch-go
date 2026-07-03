ALTER TABLE AddressEntity ADD COLUMN firstName VARCHAR(255) NULL;
ALTER TABLE AddressEntity ADD COLUMN lastName VARCHAR(255) NULL;

CREATE OR REPLACE FUNCTION check_names_null_or_not_null()
RETURNS TRIGGER AS $$
BEGIN
    IF (NEW.firstName IS NULL AND NEW.lastName IS NOT NULL) OR
       (NEW.firstName IS NOT NULL AND NEW.lastName IS NULL) THEN
        RAISE EXCEPTION 'firstName and lastName must either both be null or both be non-null';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enforce_names_null_or_not_null
BEFORE INSERT OR UPDATE ON AddressEntity
FOR EACH ROW
EXECUTE FUNCTION check_names_null_or_not_null();
