CREATE TABLE hosts (
    id VARCHAR(128) PRIMARY KEY,
    hostname VARCHAR(255) NOT NULL,
    ip INET NOT NULL,
    os VARCHAR(50),
    first_seen TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    tags JSONB DEFAULT '{}'
);

-- Indexes for faster lookups and improved query performance
CREATE INDEX idx_hosts_last_seen ON hosts(last_seen);
CREATE INDEX idx_hosts_hostname ON hosts(hostname);
CREATE INDEX idx_hosts_ip ON hosts(ip);

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
