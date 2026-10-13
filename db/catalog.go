package db

// PostgreSQL supplies normalized definitions. Object identities are qualified
// names, never OIDs. Mutable rows, sequence values, privileges and deployment
// owners are deliberately excluded; those do not describe schema structure.
const catalogSQL = `WITH namespaces AS (
 SELECT oid,nspname FROM pg_namespace WHERE left(nspname,3)<>'pg_' AND nspname<>'information_schema'
), relations AS (
 SELECT c.*,n.nspname FROM pg_class c JOIN namespaces n ON n.oid=c.relnamespace
 WHERE c.relname<>ALL($1::text[])
), objects AS (
 SELECT 'schema' kind,nspname name,jsonb_build_object('name',nspname) definition FROM namespaces
 UNION ALL
 SELECT 'relation',nspname||'.'||relname,jsonb_build_object('kind',relkind,'persistence',relpersistence,'rls',relrowsecurity,'forceRls',relforcerowsecurity,'replicaIdentity',relreplident,'options',reloptions,'partition',CASE WHEN relkind='p' THEN pg_get_partkeydef(oid) END,'bound',pg_get_expr(relpartbound,oid),'view',CASE WHEN relkind IN ('v','m') THEN pg_get_viewdef(oid,false) END) FROM relations
 UNION ALL
 SELECT 'column',r.nspname||'.'||r.relname||'.'||a.attname,jsonb_build_object('position',a.attnum,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,'default',pg_get_expr(d.adbin,d.adrelid),'collation',CASE WHEN a.attcollation<>0 THEN cn.nspname||'.'||co.collname END,'storage',a.attstorage,'compression',a.attcompression,'dimensions',a.attndims)
 FROM relations r JOIN pg_attribute a ON a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped LEFT JOIN pg_attrdef d ON d.adrelid=r.oid AND d.adnum=a.attnum LEFT JOIN pg_collation co ON co.oid=a.attcollation LEFT JOIN pg_namespace cn ON cn.oid=co.collnamespace
 UNION ALL
 SELECT 'constraint',n.nspname||'.'||COALESCE(r.relname,t.typname)||'.'||c.conname,jsonb_build_object('type',c.contype,'definition',pg_get_constraintdef(c.oid,false),'validated',c.convalidated,'deferrable',c.condeferrable,'deferred',c.condeferred)
 FROM pg_constraint c JOIN namespaces n ON n.oid=c.connamespace LEFT JOIN pg_class r ON r.oid=c.conrelid LEFT JOIN pg_type t ON t.oid=c.contypid WHERE COALESCE(r.relname,t.typname)<>ALL($1::text[])
 UNION ALL
 SELECT 'index',r.nspname||'.'||r.relname,jsonb_build_object('definition',pg_get_indexdef(i.indexrelid,0,false),'valid',i.indisvalid,'ready',i.indisready,'live',i.indislive,'immediate',i.indimmediate,'clustered',i.indisclustered,'replica',i.indisreplident)
 FROM pg_index i JOIN relations r ON r.oid=i.indexrelid JOIN pg_class parent ON parent.oid=i.indrelid WHERE parent.relname<>ALL($1::text[])
 UNION ALL
 SELECT 'sequence',r.nspname||'.'||r.relname,jsonb_build_object('type',format_type(s.seqtypid,NULL),'start',s.seqstart,'increment',s.seqincrement,'min',s.seqmin,'max',s.seqmax,'cache',s.seqcache,'cycle',s.seqcycle,'ownedBy',(SELECT pn.nspname||'.'||p.relname||'.'||a.attname FROM pg_depend d JOIN pg_class p ON p.oid=d.refobjid JOIN pg_namespace pn ON pn.oid=p.relnamespace JOIN pg_attribute a ON a.attrelid=p.oid AND a.attnum=d.refobjsubid WHERE d.classid='pg_class'::regclass AND d.objid=r.oid AND d.refclassid='pg_class'::regclass AND d.deptype IN ('a','i') LIMIT 1)) FROM pg_sequence s JOIN relations r ON r.oid=s.seqrelid
 UNION ALL
 SELECT 'function',n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('definition',replace(pg_get_functiondef(p.oid),chr(13)||chr(10),chr(10))) FROM pg_proc p JOIN namespaces n ON n.oid=p.pronamespace WHERE p.prokind IN ('f','p')
 UNION ALL
 SELECT 'aggregate',n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',jsonb_build_object('unsupported',true) FROM pg_proc p JOIN namespaces n ON n.oid=p.pronamespace WHERE p.prokind='a'
 UNION ALL
 SELECT 'operator',n.nspname||'.'||o.oprname||'('||format_type(o.oprleft,NULL)||','||format_type(o.oprright,NULL)||')',jsonb_build_object('function',o.oprcode::regprocedure::text,'result',format_type(o.oprresult,NULL),'merge',o.oprcanmerge,'hash',o.oprcanhash) FROM pg_operator o JOIN namespaces n ON n.oid=o.oprnamespace
 UNION ALL
 SELECT 'trigger',r.nspname||'.'||r.relname||'.'||t.tgname,jsonb_build_object('definition',pg_get_triggerdef(t.oid,false),'enabled',t.tgenabled) FROM pg_trigger t JOIN relations r ON r.oid=t.tgrelid WHERE NOT t.tgisinternal
 UNION ALL
 SELECT 'type',n.nspname||'.'||t.typname,jsonb_build_object('kind',t.typtype,'base',CASE WHEN t.typbasetype<>0 THEN format_type(t.typbasetype,t.typtypmod) END,'notNull',t.typnotnull,'default',t.typdefault,'enum',(SELECT jsonb_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid=t.oid)) FROM pg_type t JOIN namespaces n ON n.oid=t.typnamespace LEFT JOIN pg_class c ON c.oid=t.typrelid WHERE t.typtype IN ('e','d','r','m') OR c.relkind='c'
 UNION ALL
 SELECT 'collation',n.nspname||'.'||c.collname,jsonb_build_object('provider',c.collprovider,'deterministic',c.collisdeterministic,'encoding',c.collencoding,'collate',c.collcollate,'ctype',c.collctype,'locale',c.colllocale,'rules',c.collicurules,'version',c.collversion) FROM pg_collation c JOIN namespaces n ON n.oid=c.collnamespace
 UNION ALL
 SELECT 'inheritance',r.nspname||'.'||r.relname||'.'||i.inhseqno,jsonb_build_object('parent',n.nspname||'.'||p.relname,'detachPending',i.inhdetachpending) FROM pg_inherits i JOIN relations r ON r.oid=i.inhrelid JOIN pg_class p ON p.oid=i.inhparent JOIN pg_namespace n ON n.oid=p.relnamespace
 UNION ALL
 SELECT 'extension',e.extname,jsonb_build_object('version',e.extversion,'schema',n.nspname,'relocatable',e.extrelocatable) FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace
 UNION ALL
 SELECT 'policy',r.nspname||'.'||r.relname||'.'||p.polname,jsonb_build_object('command',p.polcmd,'permissive',p.polpermissive,'using',pg_get_expr(p.polqual,p.polrelid),'check',pg_get_expr(p.polwithcheck,p.polrelid),'roles',(SELECT jsonb_agg(CASE WHEN role=0 THEN 'public' ELSE pg_get_userbyid(role) END ORDER BY role) FROM unnest(p.polroles) role)) FROM pg_policy p JOIN relations r ON r.oid=p.polrelid
 UNION ALL
 SELECT 'rule',r.nspname||'.'||r.relname||'.'||w.rulename,jsonb_build_object('definition',pg_get_ruledef(w.oid,false),'enabled',w.ev_enabled) FROM pg_rewrite w JOIN relations r ON r.oid=w.ev_class WHERE w.rulename<>'_RETURN'
) SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'name',name,'definition',definition) ORDER BY kind,name,definition::text),'[]'::jsonb)::text FROM objects`

const emptySQL = `SELECT NOT EXISTS(
 SELECT 1 FROM pg_namespace WHERE left(nspname,3)<>'pg_' AND nspname NOT IN ('public','information_schema')
 UNION ALL SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_operator o JOIN pg_namespace n ON n.oid=o.oprnamespace WHERE n.nspname='public'
 UNION ALL SELECT 1 FROM pg_extension WHERE extname<>'plpgsql'
)`
