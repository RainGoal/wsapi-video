CREATE TABLE IF NOT EXISTS wallet_accounts (
 user_id text PRIMARY KEY REFERENCES users(id), available bigint NOT NULL DEFAULT 0 CHECK(available>=0),
 frozen bigint NOT NULL DEFAULT 0 CHECK(frozen>=0), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE model_products (
 id text PRIMARY KEY, name text NOT NULL, kind text NOT NULL CHECK(kind IN ('image','video')),
 model text NOT NULL, credits bigint NOT NULL CHECK(credits>0 AND credits<=1000000000), version integer NOT NULL DEFAULT 1,
 capabilities jsonb NOT NULL, active boolean NOT NULL DEFAULT false, verified boolean NOT NULL DEFAULT false,
 is_demo boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE quotes (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), project_id text NOT NULL REFERENCES projects(id),
 product_id text NOT NULL REFERENCES model_products(id), input jsonb NOT NULL, input_hash text NOT NULL,
 product_snapshot jsonb NOT NULL, credits bigint NOT NULL CHECK(credits>0), expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE generation_tasks (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), project_id text NOT NULL REFERENCES projects(id),
 quote_id text NOT NULL UNIQUE REFERENCES quotes(id), product_id text NOT NULL REFERENCES model_products(id), request_key text NOT NULL,
 request_hash text NOT NULL, input jsonb NOT NULL, product_snapshot jsonb NOT NULL, credits bigint NOT NULL CHECK(credits>0),
 kind text NOT NULL, model text NOT NULL, credential_ref text NOT NULL, upstream_id text,
 execution_status text NOT NULL DEFAULT 'queued', delivery_status text NOT NULL DEFAULT 'pending',
 settlement_status text NOT NULL DEFAULT 'frozen', procurement_status text NOT NULL DEFAULT 'pending',
 asset_ids jsonb NOT NULL DEFAULT '[]', outputs jsonb NOT NULL DEFAULT '[]', is_demo boolean NOT NULL DEFAULT false,
 error_code text NOT NULL DEFAULT '', error_message text NOT NULL DEFAULT '', attempts integer NOT NULL DEFAULT 0,
 lease_token text, lease_until timestamptz, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(user_id,request_key)
);
CREATE INDEX generation_tasks_worker ON generation_tasks(next_attempt_at) WHERE settlement_status='frozen';
CREATE INDEX generation_tasks_user ON generation_tasks(user_id,created_at DESC);
CREATE TABLE task_attempts (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES generation_tasks(id), attempt integer NOT NULL,
 action text NOT NULL, status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
 UNIQUE(task_id,attempt)
);
CREATE TABLE payment_tiers (
 id text PRIMARY KEY, name text NOT NULL, amount_fen bigint NOT NULL CHECK(amount_fen>0 AND amount_fen<=100000000),
 credits bigint NOT NULL CHECK(credits>0 AND credits<=1000000000), active boolean NOT NULL DEFAULT false,
 version integer NOT NULL DEFAULT 1, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE payment_orders (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), out_trade_no text NOT NULL UNIQUE,
 tier_id text NOT NULL REFERENCES payment_tiers(id), tier_version integer NOT NULL, amount_fen bigint NOT NULL CHECK(amount_fen>0),
 credits bigint NOT NULL CHECK(credits>0), method text NOT NULL, credential_ref text NOT NULL, merchant_pid text NOT NULL,
 provider_origin text NOT NULL, trade_no text, payment_status text NOT NULL DEFAULT 'pending',
 credit_status text NOT NULL DEFAULT 'pending', checkout_url text NOT NULL, expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), paid_at timestamptz, credited_at timestamptz,
 next_check_at timestamptz NOT NULL DEFAULT now()+interval '30 seconds', check_attempts integer NOT NULL DEFAULT 0,
 lease_until timestamptz
);
CREATE UNIQUE INDEX payment_orders_provider_trade ON payment_orders(provider_origin,merchant_pid,trade_no) WHERE trade_no IS NOT NULL;
CREATE INDEX payment_orders_user ON payment_orders(user_id,created_at DESC);
CREATE TABLE wallet_ledger (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), event_key text NOT NULL UNIQUE,
 event_type text NOT NULL, available_delta bigint NOT NULL, frozen_delta bigint NOT NULL,
 available_after bigint NOT NULL CHECK(available_after>=0), frozen_after bigint NOT NULL CHECK(frozen_after>=0),
 task_id text REFERENCES generation_tasks(id), order_id text REFERENCES payment_orders(id), reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_ledger_user ON wallet_ledger(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS admin_audit (
 id text PRIMARY KEY, admin_id text NOT NULL REFERENCES users(id), action text NOT NULL, target_id text NOT NULL,
 reason text NOT NULL, details jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO model_products(id,name,kind,model,credits,capabilities,active,verified,is_demo) VALUES
 ('demo-image','演示图像 · 不连接真实模型','image','demo-image',10,'{"aspect_ratios":["16:9","1:1","9:16"],"durations":[],"resolutions":[],"sizes":["1024x1024"],"qualities":["standard"],"max_images":4,"max_videos":0,"max_audios":0,"supports_audio":false,"supports_frames":false,"supports_cancel":false}',true,true,true),
 ('demo-video','演示视频流程 · 返回带标记的静态预览','video','demo-video',40,'{"aspect_ratios":["16:9","9:16"],"durations":[5],"resolutions":["720p"],"sizes":[],"qualities":[],"max_images":1,"max_videos":0,"max_audios":0,"supports_audio":false,"supports_frames":false,"supports_cancel":false}',false,false,true);
INSERT INTO payment_tiers(id,name,amount_fen,credits,active) VALUES
 ('starter','入门充值',1000,1000,false),('creator','创作者充值',5000,5500,false),('studio','工作室充值',10000,12000,false);
