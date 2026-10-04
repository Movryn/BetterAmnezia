/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// Native strings (tray menu, dialogs, notifications). The page has its own
// dictionaries in assets/i18n.js.
var nativeRU = map[string]string{
	"Open BetterAmnezia":             "Открыть BetterAmnezia",
	"No tunnels":                     "Нет туннелей",
	"connecting":                     "подключение",
	"disconnecting":                  "отключение",
	"Disconnect all":                 "Отключить все",
	"Import tunnel…":                 "Импорт туннеля…",
	"Exit":                           "Выход",
	"Connected":                      "Подключено",
	"Disconnected":                   "Отключено",
	"connect":                        "подключение",
	"disconnect":                     "отключение",
	"Tunnel error: ":                 "Ошибка туннеля: ",
	"Auto-tunnel":                    "Автотуннель",
	"Import tunnels":                 "Импорт туннелей",
	"Tunnel files":                   "Файлы туннелей",
	"All files":                      "Все файлы",
	"Export tunnels":                 "Экспорт туннелей",
	"Export tunnel":                  "Экспорт туннеля",
	"Save log":                       "Сохранить журнал",
	"Text files":                     "Текстовые файлы",
	"Programs":                       "Программы",
	"Choose applications":            "Выберите приложения",
	"Choose the split tunnel driver": "Выберите драйвер раздельного туннелирования",
	"Choose a folder; every program inside it will follow the rule.":                                  "Выберите папку: правило применится ко всем программам внутри неё.",
	"Restarting unhealthy tunnel: ":                                                                   "Перезапуск неисправного туннеля: ",
	"This action requires administrator rights.":                                                      "Для этого действия нужны права администратора.",
	"A tunnel named %s already exists.":                                                               "Туннель с именем %s уже существует.",
	"Invalid name: use up to 32 letters, digits and _=+.- characters.":                                "Недопустимое имя: до 32 латинских букв, цифр и символов _=+.-",
	"The configuration is too large for a QR code: %v":                                                "Конфигурация слишком велика для QR-кода: %v",
	"A new version is available. Click to see what's new.":                                            "Доступна новая версия. Нажмите, чтобы узнать подробности.",
	"This copy was not installed with the installer. Download the new version from the release page.": "Эта копия установлена без установщика. Скачайте новую версию на странице релиза.",
	"The update is already being downloaded.":                                                         "Обновление уже загружается.",
	"The configuration needs a peer with an endpoint.":                                                "В конфигурации нужен пир с адресом сервера.",
	"The configuration needs an interface address.":                                                   "В конфигурации нужен адрес интерфейса.",
	"The peer has no allowed IPs of the interface's address family.":                                  "У пира нет разрешённых адресов того же типа, что и адрес интерфейса.",
	"Disconnect this tunnel first: the test would interrupt its connection.":                          "Сначала отключите этот туннель: проверка прервёт его соединение.",
	"A scan is already running.":                                                                      "Проверка уже выполняется.",
	"No update is available.":                                                                         "Обновлений нет.",
	"The release has no installer for this computer.":                                                 "В релизе нет установщика для этого компьютера.",
}

var (
	russian  atomic.Bool
	langOnce sync.Once
)

func setLanguage(lang string) {
	if lang == "" || lang == "auto" {
		lang = "en"
		if langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(langs) > 0 && strings.HasPrefix(strings.ToLower(langs[0]), "ru") {
			lang = "ru"
		}
	}
	russian.Store(lang == "ru")
}

func tr(s string) string {
	langOnce.Do(func() { setLanguage(loadPrefs().Language) })
	if russian.Load() {
		if t, ok := nativeRU[s]; ok {
			return t
		}
	}
	return s
}
