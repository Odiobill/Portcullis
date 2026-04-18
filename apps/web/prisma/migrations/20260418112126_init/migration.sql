-- CreateTable
CREATE TABLE "services" (
    "id" TEXT NOT NULL,
    "domains" TEXT[],
    "upstream_container" TEXT NOT NULL,
    "upstream_port" INTEGER NOT NULL,
    "db_name" TEXT,
    "db_user" TEXT,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "services_pkey" PRIMARY KEY ("id")
);
