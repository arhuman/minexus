CREATE TABLE hosts (
    id VARCHAR(128) PRIMARY KEY,
    hostname VARCHAR(255) NOT NULL,
    ip INET NOT NULL,
    os VARCHAR(50),
    first_seen TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    tags JSONB DEFAULT '{}',
    hardware_fingerprint TEXT,
    conflict_status TEXT DEFAULT NULL
);

-- Comments for documentation
COMMENT ON COLUMN hosts.hardware_fingerprint IS 'Unique identifier based on hardware characteristics';
COMMENT ON COLUMN hosts.conflict_status IS 'Current conflict status: null (no conflict), pending, resolved, or manual_review';

-- Indexes for faster lookups and improved query performance
CREATE INDEX idx_hosts_last_seen ON hosts(last_seen);
CREATE INDEX idx_hosts_hardware_fingerprint ON hosts(hardware_fingerprint);
CREATE INDEX idx_hosts_conflict_status ON hosts(conflict_status);

-- Add a unique constraint for active hosts
CREATE UNIQUE INDEX idx_active_host_unique
ON hosts(hostname, ip)
WHERE conflict_status IS NULL;

-- Table for storing registration history
CREATE TABLE registration_history (
    id SERIAL PRIMARY KEY,
    host_id VARCHAR(128) NOT NULL REFERENCES hosts(id),
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    registration_type VARCHAR(20) NOT NULL CHECK (registration_type IN ('REGISTRATION', 'CONFLICT')),
    details JSONB NOT NULL,
    CONSTRAINT fk_registration_history_host FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE
);

-- Index for faster registration history lookups
CREATE INDEX idx_registration_history_host_id ON registration_history(host_id);
CREATE INDEX idx_registration_history_timestamp ON registration_history(timestamp);
CREATE INDEX idx_registration_history_type ON registration_history(registration_type);

-- Function to record a new registration
CREATE OR REPLACE FUNCTION record_registration(
    p_host_id VARCHAR(128),
    p_ip INET,
    p_hostname VARCHAR(255),
    p_hardware_fingerprint TEXT
) RETURNS void AS $$
BEGIN
    INSERT INTO registration_history (
        host_id,
        registration_type,
        details
    ) VALUES (
        p_host_id,
        'REGISTRATION',
        jsonb_build_object(
            'ip', p_ip,
            'hostname', p_hostname,
            'hardware_fingerprint', p_hardware_fingerprint
        )
    );
END;
$$ LANGUAGE plpgsql;

-- Function to record conflicts
CREATE OR REPLACE FUNCTION record_registration_conflict(
    p_host_id VARCHAR(128),
    p_conflict_type TEXT,
    p_resolution TEXT,
    p_details jsonb
) RETURNS void AS $$
BEGIN
    INSERT INTO registration_history (
        host_id,
        registration_type,
        details
    ) VALUES (
        p_host_id,
        'CONFLICT',
        jsonb_build_object(
            'type', p_conflict_type,
            'resolution', p_resolution,
            'details', p_details
        )
    );
END;
$$ LANGUAGE plpgsql;

-- Trigger function to automatically record registrations
CREATE OR REPLACE FUNCTION auto_record_registration()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND
        (OLD.hostname != NEW.hostname OR OLD.ip != NEW.ip OR OLD.hardware_fingerprint != NEW.hardware_fingerprint)) THEN
        PERFORM record_registration(
            NEW.id,
            NEW.ip,
            NEW.hostname,
            NEW.hardware_fingerprint
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for automatic registration recording
CREATE TRIGGER auto_registration_trigger
    AFTER INSERT OR UPDATE ON hosts
    FOR EACH ROW
    EXECUTE FUNCTION auto_record_registration();

CREATE TABLE commands (
    id VARCHAR(128) PRIMARY KEY,
    host_id VARCHAR(128) REFERENCES hosts(id),
    command TEXT NOT NULL,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    direction VARCHAR(4) CHECK (direction IN ('SENT', 'RECV')),
    status VARCHAR(20) DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'RECEIVED', 'EXECUTING', 'COMPLETED', 'FAILED'))
);

-- Index for faster status lookups
CREATE INDEX idx_commands_status ON commands(status);

-- Table for storing command execution results
CREATE TABLE command_results (
    id SERIAL PRIMARY KEY,
    command_id VARCHAR(128) NOT NULL,
    minion_id VARCHAR(128) NOT NULL,
    exit_code INTEGER NOT NULL DEFAULT 0,
    stdout TEXT,
    stderr TEXT,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_command_results_host FOREIGN KEY (minion_id) REFERENCES hosts(id),
    CONSTRAINT fk_command_results_command FOREIGN KEY (command_id) REFERENCES commands(id)
);

-- Index for faster command result lookups
CREATE INDEX idx_command_results_command_id ON command_results(command_id);
CREATE INDEX idx_command_results_minion_id ON command_results(minion_id);
CREATE INDEX idx_command_results_timestamp ON command_results(timestamp);
