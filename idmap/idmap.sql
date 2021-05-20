/****************************************
 * Mapping emails to people and companies
 ****************************************/

create table if not exists person(
    personid    integer primary key,
    personname  text not null, /* "Canonical" name for the person.  Might not be unique. */
    persondesc  text           /* Anything useful to distinguish this person from someone else w/ the same name */
);

create table if not exists company(
    companyid   integer primary key,
    companyname text not null,
    companydesc text
);

/*
 * This should only be used where the email address reliable indicates employer;
 * e.g., citrix.com -> Citrix, but gmail.com !-> Google.
 */
create table if not exists hostname_to_company(
    hostname    text not null,
    companyid   integer not null,
    unique(hostname),
    foreign key(companyid) references company
);

create table if not exists person_to_company(
    personid  integer not null,
    companyid integer not null,
    startdate date,
    enddate   date,
    unique(personid, companyid, startdate, enddate),
    foreign key(personid) references person,
    foreign key(companyid) references company
);

create table if not exists idmap.address_to_person(
    mailboxname text not null,
    hostname    text not null,
    personid integer not null,
    primary key(mailboxname, hostname)
    unique(mailboxname, hostname, personid),
    foreign key(personid) references person
);

create table if not exists idmap.tags(
    tagid integer primary key,
    tagname text not null,
    tagdesc text
);

create table if not exists idmap.address_to_tag(
    mailboxname text not null,
    hostname    text not null,
    tagid integer not null,
    primary key(mailboxname, hostname)
    unique(mailboxname, hostname, tagid),
    foreign key(tagid) references tags
);

/* Link a list of address ids to a person.  Left join so that you get
 * an error if you mistype the person name. */
insert into idmap.address_to_person(mailboxname, hostname, personid) 
    select * from (values ('JBeulich', 'suse.com'), ('jbeulich', 'suse.com'))
        left join (select personid
	          from idmap.person
		  where personname="Jan Beulich");

/* Same as above but for tags */
insert into idmap.address_to_tag(mailboxname, hostname, tagid) 
    select * from (values ('osstest-admin', 'xenproject.org'), ('citrix-osstest', 'xenproject.org'), ('osstest', 'xenbits.xen.org'))
        left join (select tagid
	          from idmap.tags
		  where tagname='bot');

