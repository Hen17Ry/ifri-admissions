-- Canonical reference data supplied for phase 2; no campaign, quota or rules.
INSERT INTO programs (code,name) VALUES
('IA', 'Intelligence Artificielle'),
('SI', 'Sécurité Informatique'),
('GL', 'Génie Logiciel'),
('IM', 'Internet et Multimédia'),
('SEIOT', 'Systèmes Embarqués et Internet des Objets')
ON CONFLICT (code) DO NOTHING;
INSERT INTO origin_qualifications (code,label,family) VALUES
('BAC_C', 'BAC C', 'BAC'),
('BAC_D', 'BAC D', 'BAC'),
('BAC_E', 'BAC E', 'BAC'),
('DT_IMI', 'DT/IMI', 'DT'),
('DT_DWM', 'DT/DWM', 'DT'),
('DT_PM', 'DT/PM', 'DT'),
('DT_EAP', 'DT/EAP', 'DT')
ON CONFLICT (code) DO NOTHING;
INSERT INTO subjects (code,name) VALUES
('MATH', 'Mathématiques'),
('APPLIED_MATH', 'Mathématiques appliquées'),
('FRENCH', 'Français'),
('ENGLISH', 'Anglais'),
('MANUFACTURING_STUDY', 'Étude de Fabrication'),
('INFORMATION_SYSTEMS_TECH', 'Technologie des systèmes informatiques'),
('WEB_SITES_APPS', 'Sites et Applications Web'),
('APPLIED_SCIENCES', 'Sciences appliquées'),
('ELECTRONICS_STUDY', 'Étude électronique'),
('IT_PROJECT', 'Projet informatique'),
('EDITING', 'Montage')
ON CONFLICT (code) DO NOTHING;

