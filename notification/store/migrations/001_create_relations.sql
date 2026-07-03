CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE UserEntity (
    id UUID PRIMARY KEY UNIQUE
);

CREATE TABLE NotificationEntity (
    id UUID PRIMARY KEY UNIQUE DEFAULT uuid_generate_v4(),
    dateSent TIMESTAMPTZ NOT NULL,
    dateRead TIMESTAMPTZ,
    title VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    userId UUID NOT NULL,
    FOREIGN KEY (userId) REFERENCES UserEntity(id)
);
