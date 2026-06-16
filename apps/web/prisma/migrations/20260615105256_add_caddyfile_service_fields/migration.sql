-- AlterTable
ALTER TABLE "services" ADD COLUMN     "service_type" TEXT NOT NULL DEFAULT 'proxy',
ADD COLUMN     "static_root" TEXT,
ADD COLUMN     "tls_mode" TEXT NOT NULL DEFAULT 'acme',
ALTER COLUMN "upstream_container" DROP NOT NULL,
ALTER COLUMN "upstream_port" DROP NOT NULL;
