# Quick HOWTO

This is a quick-and-dirty utility for importing mail from a
public-inbox [1] format directory tree (including cloned git trees)
into a localmaildb-format SQLite file.

## Get your public-inbox format directory

The minimal content required for xen-devel:

    mkdir -p xen-devel/git
	git clone git clone --mirror https://lore.kernel.org/xen-devel/0 xen-devel/git/0
	git clone git clone --mirror https://lore.kernel.org/xen-devel/0 xen-devel/git/1

## Do an import

    pubinfetch -mdb xen-devel/xen-devel.sqlite -pipath xen-devel

This may take a long time; I think it took maybe two hours the first
time.

## Doing data mining

There are a large number of sample queries in `localmaildb/localmaildb.sql`.

Many of those queries require an attached "idmap" database, which maps
(email address) -> (person) and (person, date range) -> company.

This must be created manually using your own knowledge (or borrowed
from someone willing to share theirs), and attached as 'idmap'.

A useful method I've found:

1. Run the `sqlite3` command shell to open the database

2. `attach database 'idmap.sqlite' as idmap`

3. Construct queries in a separate text file partly by
copy-and-pasting snippets from `localmaildb.sql`, then pasting it into
the command shell. (I use the `emacs` `sql-sqlite` command to make
that more integrated.

4. When you want to make a graph, run `.once -x' before running the
query. This will output the results to .CSV and open up the system
default spreadsheet on it; from there it's usually pretty
straightforward to generate a graph.

## Updating

Unfortunately I don't have an automatic update system yet.  You have to:

1. Manually cd to `xen-devel/git/1` and do a `git fetch`

2. Run pubinfetch again as above, pressing CTRL-C when it looks like
it's got all the new mail.

Obviously lots of improvements to be made.

[1] https://public-inbox.org/
