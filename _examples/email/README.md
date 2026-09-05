# email

* This is an example of an email handler showing how to forward, reply to, and send emails

## Demo

- Update `EMAIL_DOMAIN` and `VERIFIED_DESTINATION_ADDRESS` variables in `wrangler.toml` according to your domain and email addresses you'll be using to test
- Deploy worker to cloudflare
- [Enable Email routing](https://developers.cloudflare.com/email-routing/get-started/enable-email-routing/)
- In routing rules, either set the catch-all address action to send to your worker, or create custom address and set action to send to your worker
- Ensure you add the address you used for `VERIFIED_DESTINATION_ADDRESS` as a verified destination if you want to send emails to the given destination address. Complete the verification per the prompts in Cloudflare UI.
- ⚠️ IMPORTANT - When performing the testing below, ensure to send emails from a completely separate address that is not involved
 in any forwarding related to this domain. Some email clients (like Gmail) detect sending-and-forwarding-to-yourself type of behaviour and you'll likely never get expected results.
- Send an email to your address that routes to your new worker with one of the following:
  - If `Subject` contains `please reply` -> Worker will reply to your email
  - If `Subject` contains `important` -> Worker will forward message to the addressed configured in `VERIFIED_DESTINATION_ADDRESS`
  - If none of the cases above match -> Worker will send a net new message to `VERIFIED_DESTINATION_ADDRESS`

## Tips

- Consider using 3p packages like https://github.com/jhillyerd/enmime for easier construction of emails, particularly outbound ones
- Email Hygiene:
  - The From/To values in the headers must always match the From/To values of the raw email, otherwise Cloudflare will throw an error
  - Always include a Message-ID, otherwise cloudflare will throw an error
  - Keep these in mind when using `.Reply()` - all of these are necessary in order for mail clients like Gmail to properly thread the messages
    - Always include `In-Reply-To` Header referencing the original `Message-ID`
    - Always include `References` Header referencing the original `Message-ID`
    - Always ensure the Subject starts with `Re: `
  - If you are building a `mail.Header` map manually, you MUST ensure to store the header with capitalized first character. The stdlib does this via the textproto package, i.e. `textproto.CanonicalMIMEHeaderKey("message-id")`

## Known issues
### [Cannot reply to emails received on a subdomain](https://community.cloudflare.com/t/email-worker-cannot-reply-to-emails-received-on-a-subdomain/719852/6)

Can't do much about this one, as the bug exists even at the javascript layer. Workarounds are to use your TLD for emails, or use `.Send()` explicitly rather than `.Reply()` if the recipient is a verified destination address.

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Commands

```
make dev     # run dev server
make build   # build Go Wasm binary
make deploy  # deploy worker
```

