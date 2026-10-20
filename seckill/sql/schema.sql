-- 秒杀系统 MySQL DDL（MySQL 8.0, InnoDB, utf8mb4）
CREATE DATABASE IF NOT EXISTS seckill DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE seckill;

-- 秒杀活动表：一行 = 一个活动（一个商品的一场秒杀）
CREATE TABLE IF NOT EXISTS seckill_activity (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '活动 ID，自增主键，顺序写入避免页分裂',
    goods_id    BIGINT UNSIGNED NOT NULL COMMENT '商品 ID',
    title       VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '活动标题',
    price       DECIMAL(10, 2)  NOT NULL COMMENT '秒杀价，金额用 DECIMAL',
    total_stock INT UNSIGNED    NOT NULL COMMENT '初始库存',
    stock       INT UNSIGNED    NOT NULL COMMENT '数据库剩余库存（最终兜底）',
    start_time  DATETIME        NOT NULL COMMENT '开始时间',
    end_time    DATETIME        NOT NULL COMMENT '结束时间',
    status      TINYINT         NOT NULL DEFAULT 0 COMMENT '0 草稿 1 已发布 2 已下线',
    version     INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '乐观锁版本号（后台编辑用）',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_status_start (status, start_time) COMMENT '预热任务按状态 + 开始时间扫描'
) ENGINE = InnoDB COMMENT = '秒杀活动';

-- 秒杀订单表：一人一单由唯一索引兜底
CREATE TABLE IF NOT EXISTS seckill_order (
    id          BIGINT UNSIGNED NOT NULL COMMENT '订单 ID，雪花算法生成，趋势递增',
    activity_id BIGINT UNSIGNED NOT NULL COMMENT '活动 ID',
    user_id     BIGINT UNSIGNED NOT NULL COMMENT '用户 ID',
    goods_id    BIGINT UNSIGNED NOT NULL COMMENT '商品 ID（冗余，减少 JOIN）',
    price       DECIMAL(10, 2)  NOT NULL COMMENT '成交价',
    status      TINYINT         NOT NULL DEFAULT 0 COMMENT '0 待支付 1 已支付 2 已取消',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_activity (user_id, activity_id) COMMENT '一人一单；user_id 在前，兼顾「我的订单」查询',
    KEY idx_activity_status (activity_id, status) COMMENT '对账、按活动统计'
) ENGINE = InnoDB COMMENT = '秒杀订单';
