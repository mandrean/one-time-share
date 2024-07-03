## One Time Share (1ts.dev)
A simple web service written in golang that handles sharing of self-destructing messages.

[![Build](https://github.com/gameraccoon/one-time-share/actions/workflows/build.yml/badge.svg)](https://github.com/gameraccoon/one-time-share/actions/workflows/build.yml)

This service is useful when you want to share content-sensitive information in a conversation with someone, and you don't want this information to persist there.

Example:
> [me]: Hey Dave, can you unlock my computer so I can remote login into it? My pin: [URL]  
> [Dave]: Sure!

If Dave's computer gets hacked the day after, the person who would get access to it won't be able to see your pin from this conversation.

### What does the service guarantee?

- The shared message can be accessed by the link only once, the message is removed from the server even before it reaches the recipient
- The message will not be accessible after the set time and will be removed from the server soon after it expires
- TLS encryption guarantees that the message can't be intercepted on the way to the server and from the server to the recipient (except if your way of communication is prone to the "man in the middle" attack, more on that below).

### What this service does not guarantee

This service does not guarantee that the message can not be accessed by a malicious user. If your messenger is prone to the "man in the middle" attack, or if you or your recipient have spyware on your machines this server won't save you from leaking information.

Also if the link gets to a malicious user before the recipient accesses it, the data can be exposed to that malicious user.

And of course, as most of the services today, this service can't give guarantees that it never gets hacked, or that my hosting provider won't access the data on the server. Below is some info about how to deal with that.

### What limitations does the service have?

- Since the message is removed before it is sent to the recipient, the delivery is not guaranteed. If the recipient has a very bad internet connection, the message may not be delivered to them and would need to be resent.
- The service doesn't distinguish between read, expired, and non-existing messages.
- Only text messages supported, no files.
- Only HTTPS, no HTTP
- To avoid spam and overuse, there's a very strict limit set on how often the messages can be created, and the limit is shared between all users
- The size of the message is restricted as well
- The messages are not stored forever (on your own server you can disable all these limits).

### What information is stored on the server

- Message (basically in plain text)
- Time of expiry of the message
- Token associated with the message

### Can I set up my own server?

Yes. The server is quite easy to set up and run on your machine, it doesn't have a lot of dependencies, and the code should be quite simple to review.

### What about that "man in the middle" thing?

In simple words, if you exchange messages with someone over the channel which allows someone to intercept messages and potentially replace them with their own, we can say that this channel is prone to the "man in the middle" attack.

As of 2024, most of the messengers and other ways of online communication are prone to this type of attack, with the exception of the ones that use end-to-end encryption.

This service as well would store the sent messages basically in plain text (until they are accessed or expire), so if someone hacks into the server, they can intercept the messages that are in the flight.

### OK, what should I do about it?

If you do care that your information is secure, you either need to use some [end-to-end encrypted](https://en.wikipedia.org/wiki/End-to-end_encryption) messenger that you trust, or utilize [public-key cryptography](https://en.wikipedia.org/wiki/Email_encryption) yourself.

**If you don't use end-to-end encryption, your conversations can be accessed by other people.**

### What if the service gets hacked?

There's a simple rule: when sending information through this 1ts service don't add any context to your secret data.

Bad:
> Hi, by the link you will find your login and password, I've also added the URL of the website there just in case.  
> [link]

Good:
> Hi, your login is User1447, your password is by the link below. The URL is example.com.  
> [link]

This would make the difference in case our server gets hacked. in the first case, the hacker would get everything they need to access the account, and in the second case, they would get just a set of symbols without any idea where it can be applied.

## Your own server set up

1. Clone the repository
2. In `app-config.json` set paths to your TLS certificate and key, or set `forceUnprotectedHttp` to `true` in case you enable HTTPS through a reverse proxy such as nginx
3. In `app-config.json` set `port` and limits
4. `go build` to build the executable or `go run` to run it directly
6. Use `tools/run_daemon.sh` to start the service in the background or configure it to be run as you usually run services

Take a look at [build.yaml](https://github.com/gameraccoon/one-time-share/blob/main/.github/workflows/build.yml) to see how I build it.

### Things to think about when setting up your own server
- Make sure your server runs under HTTPS and is not accessible via HTTP
  - Using HTTP is as good as broadcasting your private data to everyone in your network
- Whether you plan to deploy this web service or develop your own for your business, this service can be an easy point of entry for hackers to access other systems. Therefore, you should ensure that no important information (such as access tokens or permanent passwords) is shared, and that the service is secured no less than other sensitive parts of your network.
  - It's one thing if someone hacks into my server and finds a lot of random data without context, but it's a very different situation if they can understand who the data is shared by and intended for (or potentially even more context about this information if the hackers already have access to some other systems).
