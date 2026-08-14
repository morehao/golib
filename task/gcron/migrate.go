package gcron

import "gorm.io/gorm"

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&CronTask{}, &CronTaskRun{})
}
