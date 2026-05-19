INSERT INTO knowledge_base (keyword, content) VALUES
('bifrost', 'Bifrost is an AI router and proxy that handles load balancing, retries, and plugin execution.'),
('kronk', 'Kronk is an AI gateway or model server that acts as a bridge to underlying local models.'),
('opa', 'Open Policy Agent (OPA) is an open-source, general-purpose policy engine that unifies policy enforcement.'),
('rag', 'Retrieval-Augmented Generation (RAG) provides contextual knowledge to an LLM before generating a response.')
ON CONFLICT (keyword) DO NOTHING;
