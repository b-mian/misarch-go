ALTER TABLE ProductVariantVersionEntity
ALTER COLUMN canBeReturnedForDays TYPE INT USING (
    CASE
        WHEN canBeReturnedForDays IS NULL THEN 0
        WHEN canBeReturnedForDays = 'Infinity' THEN NULL
        ELSE CAST(canBeReturnedForDays AS INT)
    END
);
