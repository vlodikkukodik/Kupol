// Package dnszones — собственный DNS Vladhost (ns.vladinc.ru и ns2.vladinc.ru на одном IP): зоны своих доменов пользователей, записи, проверка
// делегирования и автоматическая настройка почты. Панель хранит желаемое состояние в БД и отдаёт его серверу файлом dns/state.json;
// файлы зон для BIND собирает исполнитель от root (deploy/bin/dns-sync.py).
package dnszones

import "time"

// Zone — зона домена пользователя.
type Zone struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `json:"-"`
	Domain    string    `json:"domain"`
	Serial    int64     `json:"-"`
	Verified  bool      `json:"verified"` // владение подтверждено: только такая зона обслуживается на сервере
	Token     string    `json:"-"`        // код для TXT-записи подтверждения
	CreatedAt time.Time `json:"created_at"`
}

func (Zone) TableName() string { return "dns_zones" }

// Record — запись зоны.
type Record struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	ZoneID    int64     `json:"zone_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	Priority  int       `json:"priority"`
	TTL       int       `gorm:"column:ttl" json:"ttl"`
	Managed   string    `json:"managed"`
	CreatedAt time.Time `json:"created_at"`
}

func (Record) TableName() string { return "dns_records" }
