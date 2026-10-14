-- LOCAL DEVELOPMENT ONLY. Campaign, rules and null quotas; not production configuration.
-- numbering_year follows the opening date, like the migration 00006 backfill.
INSERT INTO admission_campaigns(code,name,academic_year,numbering_year,starts_at,ends_at,status)
VALUES ('DEV_LOCAL','DÉMONSTRATION LOCALE — NON OFFICIELLE','DEVELOPMENT',2000,'2000-01-01T00:00:00Z','2100-01-01T00:00:00Z','OPEN')
ON CONFLICT (code) DO NOTHING;
INSERT INTO campaign_programs(campaign_id,program_id,available_seats)
SELECT c.id,p.id,NULL FROM admission_campaigns c CROSS JOIN programs p
WHERE c.code='DEV_LOCAL' AND p.code IN ('IA','SI','GL','IM','SEIOT')
ON CONFLICT (campaign_id,program_id) DO NOTHING;
INSERT INTO rule_set_versions(campaign_program_id,version,source,status)
SELECT cp.id,1,'Matrice fournie par utilisateur, phase 2, 2026-10-12 ; fixture locale non officielle','DRAFT'
FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id WHERE c.code='DEV_LOCAL'
ON CONFLICT (campaign_program_id,version) DO NOTHING;
INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible)
SELECT v.id,q.id,true FROM (VALUES
('IA', 'BAC_C'),
('IA', 'BAC_D'),
('IA', 'BAC_E'),
('SI', 'BAC_C'),
('SI', 'BAC_D'),
('SI', 'BAC_E'),
('SI', 'DT_IMI'),
('GL', 'BAC_C'),
('GL', 'BAC_D'),
('GL', 'BAC_E'),
('GL', 'DT_IMI'),
('GL', 'DT_DWM'),
('IM', 'BAC_C'),
('IM', 'BAC_D'),
('IM', 'BAC_E'),
('IM', 'DT_IMI'),
('IM', 'DT_DWM'),
('IM', 'DT_PM'),
('SEIOT', 'BAC_C'),
('SEIOT', 'BAC_D'),
('SEIOT', 'BAC_E'),
('SEIOT', 'DT_IMI'),
('SEIOT', 'DT_EAP')
) AS fixture(program,qualification)
JOIN programs p ON p.code=fixture.program
JOIN origin_qualifications q ON q.code=fixture.qualification
JOIN campaign_programs cp ON cp.program_id=p.id
JOIN admission_campaigns c ON c.id=cp.campaign_id AND c.code='DEV_LOCAL'
JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.version=1
WHERE NOT EXISTS (SELECT 1 FROM program_qualification_eligibilities e WHERE e.rule_set_version_id=v.id AND e.origin_qualification_id=q.id);
INSERT INTO expected_subject_rules(eligibility_id,subject_id,position)
SELECT e.id,s.id,fixture.position::integer FROM (VALUES
('IA', 'BAC_C', 'MATH', '1'),
('IA', 'BAC_C', 'FRENCH', '2'),
('IA', 'BAC_C', 'ENGLISH', '3'),
('IA', 'BAC_D', 'MATH', '1'),
('IA', 'BAC_D', 'FRENCH', '2'),
('IA', 'BAC_D', 'ENGLISH', '3'),
('IA', 'BAC_E', 'MATH', '1'),
('IA', 'BAC_E', 'FRENCH', '2'),
('IA', 'BAC_E', 'MANUFACTURING_STUDY', '3'),
('SI', 'BAC_C', 'MATH', '1'),
('SI', 'BAC_C', 'FRENCH', '2'),
('SI', 'BAC_C', 'ENGLISH', '3'),
('SI', 'BAC_D', 'MATH', '1'),
('SI', 'BAC_D', 'FRENCH', '2'),
('SI', 'BAC_D', 'ENGLISH', '3'),
('SI', 'BAC_E', 'MATH', '1'),
('SI', 'BAC_E', 'FRENCH', '2'),
('SI', 'BAC_E', 'MANUFACTURING_STUDY', '3'),
('SI', 'DT_IMI', 'APPLIED_MATH', '1'),
('SI', 'DT_IMI', 'FRENCH', '2'),
('SI', 'DT_IMI', 'INFORMATION_SYSTEMS_TECH', '3'),
('GL', 'BAC_C', 'MATH', '1'),
('GL', 'BAC_C', 'FRENCH', '2'),
('GL', 'BAC_C', 'ENGLISH', '3'),
('GL', 'BAC_D', 'MATH', '1'),
('GL', 'BAC_D', 'FRENCH', '2'),
('GL', 'BAC_D', 'ENGLISH', '3'),
('GL', 'BAC_E', 'MATH', '1'),
('GL', 'BAC_E', 'FRENCH', '2'),
('GL', 'BAC_E', 'MANUFACTURING_STUDY', '3'),
('GL', 'DT_IMI', 'APPLIED_MATH', '1'),
('GL', 'DT_IMI', 'FRENCH', '2'),
('GL', 'DT_IMI', 'INFORMATION_SYSTEMS_TECH', '3'),
('GL', 'DT_DWM', 'APPLIED_MATH', '1'),
('GL', 'DT_DWM', 'FRENCH', '2'),
('GL', 'DT_DWM', 'WEB_SITES_APPS', '3'),
('IM', 'BAC_C', 'MATH', '1'),
('IM', 'BAC_C', 'FRENCH', '2'),
('IM', 'BAC_C', 'ENGLISH', '3'),
('IM', 'BAC_D', 'MATH', '1'),
('IM', 'BAC_D', 'FRENCH', '2'),
('IM', 'BAC_D', 'ENGLISH', '3'),
('IM', 'BAC_E', 'MATH', '1'),
('IM', 'BAC_E', 'FRENCH', '2'),
('IM', 'BAC_E', 'MANUFACTURING_STUDY', '3'),
('IM', 'DT_IMI', 'APPLIED_MATH', '1'),
('IM', 'DT_IMI', 'FRENCH', '2'),
('IM', 'DT_IMI', 'INFORMATION_SYSTEMS_TECH', '3'),
('IM', 'DT_DWM', 'APPLIED_MATH', '1'),
('IM', 'DT_DWM', 'FRENCH', '2'),
('IM', 'DT_DWM', 'WEB_SITES_APPS', '3'),
('IM', 'DT_PM', 'IT_PROJECT', '1'),
('IM', 'DT_PM', 'FRENCH', '2'),
('IM', 'DT_PM', 'EDITING', '3'),
('SEIOT', 'BAC_C', 'MATH', '1'),
('SEIOT', 'BAC_C', 'FRENCH', '2'),
('SEIOT', 'BAC_C', 'ENGLISH', '3'),
('SEIOT', 'BAC_D', 'MATH', '1'),
('SEIOT', 'BAC_D', 'FRENCH', '2'),
('SEIOT', 'BAC_D', 'ENGLISH', '3'),
('SEIOT', 'BAC_E', 'MATH', '1'),
('SEIOT', 'BAC_E', 'FRENCH', '2'),
('SEIOT', 'BAC_E', 'MANUFACTURING_STUDY', '3'),
('SEIOT', 'DT_IMI', 'APPLIED_MATH', '1'),
('SEIOT', 'DT_IMI', 'FRENCH', '2'),
('SEIOT', 'DT_IMI', 'INFORMATION_SYSTEMS_TECH', '3'),
('SEIOT', 'DT_EAP', 'APPLIED_SCIENCES', '1'),
('SEIOT', 'DT_EAP', 'FRENCH', '2'),
('SEIOT', 'DT_EAP', 'ELECTRONICS_STUDY', '3')
) AS fixture(program,qualification,subject,position)
JOIN programs p ON p.code=fixture.program
JOIN origin_qualifications q ON q.code=fixture.qualification
JOIN subjects s ON s.code=fixture.subject
JOIN campaign_programs cp ON cp.program_id=p.id
JOIN admission_campaigns c ON c.id=cp.campaign_id AND c.code='DEV_LOCAL'
JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.version=1
JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id AND e.origin_qualification_id=q.id
WHERE NOT EXISTS (SELECT 1 FROM expected_subject_rules existing WHERE existing.eligibility_id=e.id AND existing.subject_id=s.id);
UPDATE rule_set_versions SET status='PUBLISHED' WHERE status='DRAFT' AND version=1
AND campaign_program_id IN (SELECT cp.id FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id WHERE c.code='DEV_LOCAL');
