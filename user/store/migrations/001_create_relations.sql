-- Final schema after the original Flyway migrations V1_1 (create) and V1_2
-- (rename name -> firstName, add lastName). A fresh database only ever sees the
-- end state, so both are collapsed into this single migration. Unquoted
-- identifiers fold to lowercase in Postgres (physical table `userentity`,
-- columns `firstname`, `lastname`, `datejoined`, ...).
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE UserEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    username VARCHAR(255) NOT NULL,
    firstName VARCHAR(255) NOT NULL,
    lastName VARCHAR(255) NOT NULL,
    gender VARCHAR(255),
    birthday TIMESTAMPTZ,
    dateJoined TIMESTAMPTZ NOT NULL
);
