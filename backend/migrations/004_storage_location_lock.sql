CREATE TABLE runtime_storage_lock (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 location_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
