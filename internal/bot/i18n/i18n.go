package i18n

import (
	"fmt"
)

type Language string

const (
	EN Language = "en"
	RU Language = "ru"
)

type TranslationBundle struct {
	WelcomeNewUser    string
	WelcomeActiveUser string
	TrialActivated    string
	NoTrialAvailable  string
	SubscriptionInfo  string
	SelectPlan        string
	SelectDuration    string
	SelectPayment     string
	InvoiceCreated    string
	KeysResetSuccess  string
	NodeListHeader    string
	PromoPrompt       string
	PromoSuccess      string
	PromoInvalid      string
	HelpText          string
	BannedMessage     string
	QuotaWarning80    string
	ExpiryWarning24h  string
	ExpiryWarning72h  string
	BtnGetTrial       string
	BtnStatus         string
	BtnRenew          string
	BtnResetKeys      string
	BtnNodes          string
	BtnHelp           string
	BtnEnterPromo      string
	BtnBack            string
	ResetConfirmPrompt string
	BtnConfirmReset    string
	BtnCancelReset     string
	BtnDeviceWizard    string
	DeviceWizardTitle  string
	BtnConnectOneClick string
	BtnDownloadApp     string
	BtnReferral        string
	ReferralInfo       string
	BtnShareReferral   string
}

var Translations = map[Language]TranslationBundle{
	EN: {
		WelcomeNewUser: "Welcome to Simple-VPN-Builder secure tunnel access.\n\nEnjoy uncapped, censorship-resistant connectivity powered by VLESS Reality and AmneziaWG.",
		WelcomeActiveUser: "Simple-VPN-Builder Account Dashboard\n\nStatus: Active\nTraffic: %s / %s\nExpires: %s\nUniversal Sub URL: %s",
		TrialActivated: "Free Trial Activated Successfully!\n\nYour account has been granted %s bandwidth valid for %d hours.\n\nUniversal Subscription URL:\n`%s`\n\nTap below to copy your connection URL or scan the QR code.",
		NoTrialAvailable: "There is currently no active free trial tier configured on the network. Please choose one of the available subscription plans.",
		SubscriptionInfo: "Your Subscription Status:\n\nUser ID: %s\nUsername: @%s\nPlan: %s\nTraffic Used: %s\nTraffic Quota: %s\nExpiration Date: %s\nSub URL: `%s`",
		SelectPlan: "Choose a subscription plan to proceed with access renewal:",
		SelectDuration: "Select access duration for plan '%s':",
		SelectPayment: "Select your preferred payment gateway:",
		InvoiceCreated: "Invoice generated successfully.\n\nOrder ID: %s\nAmount: %s %s\nGateway: %s\n\nPlease complete payment using the link below:",
		KeysResetSuccess: "Your encryption credentials and subscription token have been rotated across all edge nodes.\n\nNew Universal Subscription URL:\n`%s`\n\nPlease update your VPN client configuration.",
		NodeListHeader: "Active Edge Network Nodes:\n\n%s",
		PromoPrompt: "Please reply with your discount or referral promo code:",
		PromoSuccess: "Promo code '%s' applied successfully! Discount or bonus days will be credited.",
		PromoInvalid: "The promo code provided is invalid or has expired.",
		HelpText: "Simple-VPN-Builder Client Quick Setup:\n\n1. Install a supported client:\n   - iOS: Happ, Streisand, FoXray\n   - Android: v2rayNG, sing-box, Clash Meta\n   - Desktop: v2rayN, Clash Verge Rev, Hiddify\n\n2. Import your Universal Subscription URL:\n   - Copy your subscription link from Status\n   - Open client -> Add Subscription -> Paste link -> Update config\n\n3. Connect to your preferred node.\n\nCommands:\n/start - Main menu\n/status - Quota and subscription info\n/buy - Purchase or renew plan\n/reset - Rotate all keys\n/servers - Live node list\n/promo - Apply promo code\n/help - Setup instructions",
		BannedMessage: "Your account has been suspended by network administration.\nReason: %s",
		QuotaWarning80: "Bandwidth Notice: You have consumed over 80%% of your traffic quota (%s / %s). Consider renewing your subscription to avoid service interruption.",
		ExpiryWarning24h:   "Subscription Expiration Alert: Your VPN subscription expires in less than 24 hours (%s). Tap /buy to extend your access.",
		ExpiryWarning72h:   "Subscription Notice: Your VPN subscription will expire in 3 days (%s). Tap /buy to renew seamlessly.",
		BtnGetTrial:        "Get Free Trial",
		BtnStatus:          "My Subscription",
		BtnRenew:           "Renew / Upgrade",
		BtnResetKeys:       "Rotate Keys / Reset Connection",
		BtnNodes:           "Server List",
		BtnHelp:            "Setup Guide",
		BtnEnterPromo:      "Enter Promo Code",
		BtnBack:            "<< Back",
		ResetConfirmPrompt: "Are you sure? All current configs and keys will stop working immediately.\n\nYou will receive a new configuration file and updated subscription links.",
		BtnConfirmReset:    "Yes, Rotate Keys",
		BtnCancelReset:     "Cancel",
		BtnDeviceWizard:    "Device Setup Wizard",
		DeviceWizardTitle:  "Select your platform to view step-by-step setup guide and 1-click import:",
		BtnConnectOneClick: "1-Click Connect",
		BtnDownloadApp:     "Download App",
		BtnReferral:        "Referral Program",
		ReferralInfo:       "Referral Program\n\nInvite friends and receive bonus subscription days!\nEach time an invited friend purchases access, you automatically receive +7 days of free service.\n\nYour Referral Link:\n`%s`\n\nInvited Friends: %d",
		BtnShareReferral:   "Share Referral Link",
	},
	RU: {
		WelcomeNewUser:    "Добро пожаловать в Simple-VPN-Builder.\n\nБыстрый и надежный доступ без блокировок на протоколах VLESS Reality и AmneziaWG.",
		WelcomeActiveUser: "Панель управления Simple-VPN-Builder\n\nСтатус: Активен\nТрафик: %s / %s\nИстекает: %s\nСсылка на подписку: %s",
		TrialActivated:    "Бесплатный пробный период активирован!\n\nВам начислено %s трафика на %d часов.\n\nУниверсальная ссылка на подписку:\n`%s`\n\nНажмите ниже, чтобы скопировать или получить QR-код.",
		NoTrialAvailable:  "В данный момент бесплатный пробный период не активен. Выберите подходящий тарифный план.",
		SubscriptionInfo:  "Информация о подписке:\n\nID: %s\nПользователь: @%s\nТариф: %s\nИзрасходовано: %s\nЛимит: %s\nДействует до: %s\nСсылка: `%s`",
		SelectPlan:        "Выберите подходящий тариф:",
		SelectDuration:    "Выберите период продления для тарифа '%s':",
		SelectPayment:     "Выберите способ оплаты:",
		InvoiceCreated:    "Счет на оплату сформирован.\n\nЗаказ: %s\nСумма: %s %s\nСпособ: %s\n\nПерейдите по ссылке для завершения оплаты:",
		KeysResetSuccess:  "Ключи шифрования и ссылка на подписку успешно обновлены на всех серверах.\n\nНовая ссылка:\n`%s`\n\nОбновите подписку в вашем VPN-клиенте.",
		NodeListHeader:    "Список доступных серверов:\n\n%s",
		PromoPrompt:       "Введите ваш промокод или реферальный код:",
		PromoSuccess:      "Промокод '%s' успешно применен!",
		PromoInvalid:      "Указанный промокод не существует или срок его действия истек.",
		HelpText:          "Инструкция по подключению Simple-VPN-Builder:\n\n1. Установите приложение:\n   - iOS: Happ, Streisand, FoXray\n   - Android: v2rayNG, sing-box, Clash Meta\n   - Компьютер: v2rayN, Clash Verge Rev, Hiddify\n\n2. Добавьте подписку:\n   - Скопируйте ссылку из раздела 'Моя подписка'\n   - Откройте приложение -> Добавить подписку -> Вставьте ссылку -> Обновить\n\n3. Подключитесь к серверу.\n\nКоманды бота:\n/start - Главное меню\n/status - Данные подписки и трафик\n/buy - Продление и покупка\n/reset - Сброс ключей\n/servers - Список серверов\n/promo - Ввести промокод\n/ref - Реферальная программа\n/help - Инструкция",
		BannedMessage:     "Ваш аккаунт заблокирован администрацией.\nПричина: %s",
		QuotaWarning80:    "Внимание: вы израсходовали более 80%% лимита трафика (%s / %s). Продлите подписку (/buy), чтобы не потерять доступ.",
		ExpiryWarning24h:  "Внимание: срок действия вашей подписки истекает менее чем через 24 часа (%s). Продлите доступ (/buy).",
		ExpiryWarning72h:  "Напоминание: ваша подписка истекает через 3 дня (%s). Продлите тариф (/buy) для непрерывной работы.",
		BtnGetTrial:        "Попробовать бесплатно",
		BtnStatus:          "Моя подписка",
		BtnRenew:           "Продлить / Купить",
		BtnResetKeys:       "Перевыпустить ключи / Сбросить подключение",
		BtnNodes:           "Список серверов",
		BtnHelp:            "Инструкция",
		BtnEnterPromo:      "Ввести промокод",
		BtnBack:            "<< Назад",
		ResetConfirmPrompt: "Вы уверены? Старые конфиги перестанут работать.\n\nВсе активные подключения будут прерваны, а ключи аннулированы. Вы получите новый конфигурационный файл и ссылку на подписку.",
		BtnConfirmReset:    "Да, перевыпустить",
		BtnCancelReset:     "Отмена",
		BtnDeviceWizard:    "Подключение по устройствам",
		DeviceWizardTitle:  "Выберите платформу для просмотра пошаговой инструкции и подключения в 1 клик:",
		BtnConnectOneClick: "Подключить в 1 клик",
		BtnDownloadApp:     "Скачать приложение",
		BtnReferral:        "Реферальная программа",
		ReferralInfo:       "Реферальная программа\n\nПриглашайте друзей и получайте бонусные дни подписки!\nЗа каждую оплату приглашенного друга вы автоматически получаете +7 дней бесплатного доступа.\n\nВаша реферальная ссылка:\n`%s`\n\nПриглашено друзей: %d",
		BtnShareReferral:   "Поделиться ссылкой",
	},
}

func GetBundle(lang Language) TranslationBundle {
	if b, ok := Translations[lang]; ok {
		return b
	}
	return Translations[EN]
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
