-- Keep existing logical image names and introduce immutable ciphertext generations.
ALTER TABLE image_payload
 MODIFY payload MEDIUMBLOB NULL,
 ADD COLUMN active_generation BINARY(16) DEFAULT NULL,
 ADD CONSTRAINT ck_image_payload_generation CHECK (
  (active_generation IS NULL AND payload IS NOT NULL) OR
  (active_generation IS NOT NULL AND payload IS NULL)
 );
CREATE TABLE image_payload_generation (
 name VARCHAR(255) NOT NULL,
 generation BINARY(16) NOT NULL,
 payload MEDIUMBLOB NOT NULL,
 PRIMARY KEY (name,generation),
 CONSTRAINT fk_image_payload_generation_image FOREIGN KEY (name) REFERENCES image_payload(name) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
