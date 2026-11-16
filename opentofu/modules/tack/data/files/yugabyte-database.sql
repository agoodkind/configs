SELECT 'CREATE DATABASE tack'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'tack')
\gexec
