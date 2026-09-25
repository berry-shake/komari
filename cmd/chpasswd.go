package cmd

import (
	"io"
	"os"
	"strings"

	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/accounts"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/spf13/cobra"
)

var (
	//Username    string
	NewPassword   string
	passwordStdin bool
)

var ChpasswdCmd = &cobra.Command{
	Use:     "chpasswd",
	Short:   "Force change password",
	Long:    `Force change password`,
	Example: `komari chpasswd -p <password>`,
	Run: func(cmd *cobra.Command, args []string) {
		if passwordStdin {
			data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 4097))
			if err != nil || len(data) > 4096 {
				cmd.Println("Unable to read password (maximum 4096 bytes)")
				return
			}
			NewPassword = strings.TrimRight(string(data), "\r\n")
		}
		if NewPassword == "" {
			cmd.Help()
			return
		}
		if _, err := os.Stat(flags.DatabaseFile); os.IsNotExist(err) {
			cmd.Println("Database file does not exist.")
			return
		}
		user := &models.User{}
		dbcore.GetDBInstance().Model(&models.User{}).First(user)
		cmd.Println("Changing password for user:", user.Username)
		if err := accounts.ForceResetPassword(user.Username, NewPassword); err != nil {
			cmd.Println("Error:", err)
			return
		}
		cmd.Println("Password changed successfully.")
		NewPassword = ""

		if err := accounts.DeleteAllSessions(); err != nil {
			cmd.Println("Unable to force logout of other devices:", err)
			return
		}

		cmd.Println("Please restart the server to apply the changes.")
	},
}

func init() {
	//ChpasswdCmd.PersistentFlags().StringVarP(&Username, "user", "u", "admin", "The username of the account to change password")
	ChpasswdCmd.PersistentFlags().StringVarP(&NewPassword, "password", "p", "", "New password")
	ChpasswdCmd.PersistentFlags().BoolVar(&passwordStdin, "password-stdin", false, "Read new password from standard input")
	ChpasswdCmd.MarkFlagsMutuallyExclusive("password", "password-stdin")
	RootCmd.AddCommand(ChpasswdCmd)
}
