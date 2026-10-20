// Package store 是 MySQL 访问层（database/sql + go-sql-driver/mysql）。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/lisa/seckill/internal/model"
)

var (
	// ErrNotFound 记录不存在。
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicateOrder 违反唯一索引 uk_user_activity，即同一用户重复下单。
	ErrDuplicateOrder = errors.New("store: duplicate order")
	// ErrSoldOut 数据库库存已为 0（条件更新影响 0 行）。
	ErrSoldOut = errors.New("store: sold out")
	// ErrVersionConflict 乐观锁版本冲突。
	ErrVersionConflict = errors.New("store: version conflict")
)

// mysqlErrDupEntry 是 MySQL 唯一键冲突错误码。
const mysqlErrDupEntry = 1062

// Store 封装数据库操作。
type Store struct{ db *sql.DB }

// Open 打开连接池并设置池参数。
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(100) // 上限要小于 MySQL max_connections / 实例数
	db.SetMaxIdleConns(50)
	db.SetConnMaxLifetime(30 * time.Minute) // 小于服务端 wait_timeout
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &Store{db: db}, nil
}

// Ping 检查连通性。
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close 关闭连接池。
func (s *Store) Close() error { return s.db.Close() }

const activityCols = `id, goods_id, title, price, total_stock, stock, start_time, end_time, status, version`

func scanActivity(row interface{ Scan(...any) error }) (*model.Activity, error) {
	var a model.Activity
	err := row.Scan(&a.ID, &a.GoodsID, &a.Title, &a.Price, &a.TotalStock, &a.Stock,
		&a.StartTime, &a.EndTime, &a.Status, &a.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// GetActivity 按主键查询活动（聚簇索引，一次 B+ 树查找）。
func (s *Store) GetActivity(ctx context.Context, id int64) (*model.Activity, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+activityCols+` FROM seckill_activity WHERE id = ?`, id)
	return scanActivity(row)
}

// ListUpcoming 查询即将开始或进行中的已发布活动，命中索引 idx_status_start。
func (s *Store) ListUpcoming(ctx context.Context, before time.Time) ([]*model.Activity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+activityCols+` FROM seckill_activity
		 WHERE status = ? AND start_time <= ? AND end_time > NOW()`,
		model.ActivityPublished, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*model.Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// UpdateActivityInfo 后台修改活动标题/价格，使用版本号乐观锁。
func (s *Store) UpdateActivityInfo(ctx context.Context, a *model.Activity) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE seckill_activity SET title = ?, price = ?, version = version + 1
		 WHERE id = ? AND version = ?`, a.Title, a.Price, a.ID, a.Version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrVersionConflict
	}
	return nil
}

// CreateOrder 在一个事务内创建订单并扣减数据库库存。
//
//  1. 先 INSERT 订单：唯一索引 uk_user_activity 保证一人一单，冲突立即返回，不碰热点行；
//  2. 后 UPDATE 库存：WHERE stock > 0 的条件更新是防超卖的最后一道防线；
//     UPDATE 会对活动行加 X 型记录锁，放在事务最后执行，持锁时间最短。
func (s *Store) CreateOrder(ctx context.Context, o *model.Order) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO seckill_order (id, activity_id, user_id, goods_id, price, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		o.ID, o.ActivityID, o.UserID, o.GoodsID, o.Price, model.OrderUnpaid); err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlErrDupEntry {
			return ErrDuplicateOrder
		}
		return fmt.Errorf("insert order: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE seckill_activity SET stock = stock - 1 WHERE id = ? AND stock > 0`, o.ActivityID)
	if err != nil {
		return fmt.Errorf("deduct stock: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		err = ErrSoldOut
		return err
	}
	return tx.Commit()
}

// GetOrderByUser 通过唯一索引查询用户在某活动下的订单。
func (s *Store) GetOrderByUser(ctx context.Context, aid, uid int64) (*model.Order, error) {
	var o model.Order
	err := s.db.QueryRowContext(ctx,
		`SELECT id, activity_id, user_id, goods_id, price, status, created_at
		 FROM seckill_order WHERE user_id = ? AND activity_id = ?`, uid, aid).
		Scan(&o.ID, &o.ActivityID, &o.UserID, &o.GoodsID, &o.Price, &o.Status, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

// CountOrders 统计活动的有效订单数（对账用，走 idx_activity_status 覆盖索引）。
func (s *Store) CountOrders(ctx context.Context, aid int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM seckill_order WHERE activity_id = ? AND status <> ?`,
		aid, model.OrderCanceled).Scan(&n)
	return n, err
}
